package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/png"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/neonphnx/NeonServices/internal/config"
	"github.com/neonphnx/NeonServices/internal/database"
	"github.com/neonphnx/NeonServices/internal/sshutil"
)

type ManagerApp struct {
	window     fyne.Window
	httpClient *http.Client
	sshClient  *sshutil.Client

	// Session State
	apiBaseURL      string
	authToken       string
	currentUser     *database.User
	userStatusLabel *widget.Label
	loginBtn        *widget.Button
	registerBtn     *widget.Button
	logoutBtn       *widget.Button

	// SSH Connection inputs
	hostEntry *widget.Entry
	portEntry *widget.Entry
	userEntry *widget.Entry
	keyEntry  *widget.Entry
	passEntry *widget.Entry
	connLabel *widget.Label

	// Service controls
	serviceStatusLabel *widget.Label
	logsEntry          *widget.Entry

	// NAS Monitor
	nasStatusText *widget.Label

	// My Profile
	profileInfoLabel *widget.Label
	apiKeyEntry      *widget.Entry

	// User Devices Management (reTerminal E1001)
	deviceSelect               *widget.Select
	displayDeviceIDEntry       *widget.Entry
	displayCityEntry           *widget.Entry
	displayLatEntry            *widget.Entry
	displayLonEntry            *widget.Entry
	displayTimezoneEntry       *widget.Entry
	displayCalURLEntry         *widget.Entry
	displayFullRefreshEntry    *widget.Entry
	displayPartialRefreshEntry *widget.Entry
	displayUrlLabel            *widget.Label

	// User Data / Cloud Files
	storageUsageLabel  *widget.Label
	filesListContainer *fyne.Container

	// Admin User Management
	usersTableContainer *fyne.Container

	// Admin System Settings
	mountPathEntry    *widget.Entry
	settingsPortEntry *widget.Entry
	quotaEntry        *widget.Entry
	uploadEntry       *widget.Entry
	allowRegCheck     *widget.Check
	healthStatusLabel *widget.Label

	// Main Tabs container
	tabs *container.AppTabs
}

func main() {
	a := app.NewWithID("com.neonservices.manager")
	w := a.NewWindow("NeonServices Control Center")
	w.Resize(fyne.NewSize(1100, 780))

	m := &ManagerApp{
		window:     w,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		apiBaseURL: "http://192.168.3.54:8080",
	}

	w.SetContent(m.buildUI())
	w.ShowAndRun()
}

// showWideDialog creates a responsive, appropriately sized custom modal
func showWideDialog(title, confirmText, dismissText string, box fyne.CanvasObject, minW, minH float32, parent fyne.Window, onConfirm func(bool)) {
	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(minW, minH))
	content := container.NewStack(spacer, container.NewPadded(box))

	d := dialog.NewCustomConfirm(title, confirmText, dismissText, content, onConfirm, parent)
	d.Show()
	d.Resize(fyne.NewSize(minW+40, minH+90))
}

func (m *ManagerApp) buildUI() fyne.CanvasObject {
	m.userStatusLabel = widget.NewLabel("Status: Logged Out (Public Guest)")
	m.loginBtn = widget.NewButtonWithIcon("Log In", theme.LoginIcon(), func() {
		m.showLoginDialog()
	})
	m.registerBtn = widget.NewButtonWithIcon("Register", theme.ContentAddIcon(), func() {
		m.showRegisterDialog()
	})
	m.logoutBtn = widget.NewButtonWithIcon("Log Out", theme.LogoutIcon(), func() {
		m.logout()
	})
	m.logoutBtn.Hide()

	brandText := canvas.NewText("⚡ NEON SERVICES", color.NRGBA{R: 0, G: 215, B: 255, A: 255})
	brandText.TextSize = 16
	brandText.TextStyle = fyne.TextStyle{Bold: true}

	topBar := container.NewBorder(
		nil, nil,
		container.NewHBox(brandText, widget.NewLabel(" | Enterprise NAS & Device Hub")),
		container.NewHBox(m.userStatusLabel, m.loginBtn, m.registerBtn, m.logoutBtn),
	)

	m.tabs = container.NewAppTabs(
		container.NewTabItemWithIcon("My Profile", theme.AccountIcon(), m.buildProfileTab()),
		container.NewTabItemWithIcon("My Devices", theme.VisibilityIcon(), m.buildDeviceTab()),
		container.NewTabItemWithIcon("My Cloud Files", theme.FolderIcon(), m.buildUserDataTab()),
		container.NewTabItemWithIcon("User Management (Admin)", theme.ListIcon(), m.buildUsersTab()),
		container.NewTabItemWithIcon("System Settings (Admin)", theme.SettingsIcon(), m.buildSettingsTab()),
		container.NewTabItemWithIcon("SSH & Host Service", theme.ComputerIcon(), m.buildServiceTab()),
		container.NewTabItemWithIcon("NAS Mount", theme.StorageIcon(), m.buildNASTab()),
	)
	m.tabs.SetTabLocation(container.TabLocationTop)

	return container.NewBorder(
		container.NewVBox(topBar, widget.NewSeparator()),
		nil, nil, nil,
		m.tabs,
	)
}

// ==============================================================================
// Authentication & Session
// ==============================================================================

func (m *ManagerApp) showLoginDialog() {
	userEntry := widget.NewEntry()
	userEntry.SetText("admin")
	userEntry.SetPlaceHolder("admin or your username")
	passEntry := widget.NewPasswordEntry()
	passEntry.SetPlaceHolder("password (admin default: AdminPassword123!)")
	urlEntry := widget.NewEntry()
	urlEntry.SetText(m.apiBaseURL)

	form := widget.NewForm(
		widget.NewFormItem("Server API URL", urlEntry),
		widget.NewFormItem("Username", userEntry),
		widget.NewFormItem("Password", passEntry),
	)

	header := container.NewVBox(
		widget.NewLabelWithStyle("Sign In to Your NeonServices Account", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Default admin credentials: admin / AdminPassword123!", fyne.TextAlignCenter, fyne.TextStyle{Italic: true}),
		widget.NewSeparator(),
	)

	box := container.NewVBox(header, form)

	showWideDialog("Log In to NeonServices", "Login", "Cancel", box, 560, 260, m.window, func(confirmed bool) {
		if !confirmed || userEntry.Text == "" || passEntry.Text == "" {
			return
		}
		m.apiBaseURL = strings.TrimRight(urlEntry.Text, "/")

		go func() {
			payload := map[string]string{
				"username": userEntry.Text,
				"password": passEntry.Text,
			}
			bodyBytes, _ := json.Marshal(payload)
			resp, err := m.httpClient.Post(m.apiBaseURL+"/api/v1/auth/login", "application/json", bytes.NewReader(bodyBytes))
			if err != nil {
				fyne.Do(func() {
					dialog.ShowError(fmt.Errorf("Failed to connect to %s: %v", m.apiBaseURL, err), m.window)
				})
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				data, _ := io.ReadAll(resp.Body)
				fyne.Do(func() {
					dialog.ShowError(fmt.Errorf("Login failed: %s", string(data)), m.window)
				})
				return
			}

			var authRes struct {
				Success bool `json:"success"`
				Data    struct {
					Token string         `json:"token"`
					User  *database.User `json:"user"`
				} `json:"data"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&authRes); err != nil || authRes.Data.User == nil {
				fyne.Do(func() {
					dialog.ShowError(fmt.Errorf("Failed to parse server response"), m.window)
				})
				return
			}

			fyne.Do(func() {
				m.authToken = authRes.Data.Token
				m.currentUser = authRes.Data.User

				m.userStatusLabel.SetText(fmt.Sprintf("Logged in: %s (%s)", m.currentUser.Username, strings.ToUpper(string(m.currentUser.Role))))
				m.loginBtn.Hide()
				m.registerBtn.Hide()
				m.logoutBtn.Show()

				m.refreshProfileView()
				m.refreshDeviceList()
				m.refreshUserFiles()
				if m.currentUser.Role == database.RoleAdmin {
					m.refreshUsersList()
				}

				dialog.ShowInformation("Welcome", fmt.Sprintf("Successfully logged in as %s (%s)", m.currentUser.Username, m.currentUser.Role), m.window)
			})
		}()
	})
}

func (m *ManagerApp) showRegisterDialog() {
	userEntry := widget.NewEntry()
	userEntry.SetPlaceHolder("new_username")
	emailEntry := widget.NewEntry()
	emailEntry.SetPlaceHolder("user@neonservices.local")
	passEntry := widget.NewPasswordEntry()
	passEntry.SetPlaceHolder("at least 8 characters")
	urlEntry := widget.NewEntry()
	urlEntry.SetText(m.apiBaseURL)

	form := widget.NewForm(
		widget.NewFormItem("Server API URL", urlEntry),
		widget.NewFormItem("Username", userEntry),
		widget.NewFormItem("Email", emailEntry),
		widget.NewFormItem("Password", passEntry),
	)

	header := container.NewVBox(
		widget.NewLabelWithStyle("Register New NeonServices Account", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),
	)

	box := container.NewVBox(header, form)

	showWideDialog("Register Account", "Register", "Cancel", box, 560, 290, m.window, func(confirmed bool) {
		if !confirmed || userEntry.Text == "" || passEntry.Text == "" {
			return
		}
		m.apiBaseURL = strings.TrimRight(urlEntry.Text, "/")

		go func() {
			payload := map[string]string{
				"username": userEntry.Text,
				"email":    emailEntry.Text,
				"password": passEntry.Text,
			}
			bodyBytes, _ := json.Marshal(payload)
			resp, err := m.httpClient.Post(m.apiBaseURL+"/api/v1/auth/register", "application/json", bytes.NewReader(bodyBytes))
			if err != nil {
				fyne.Do(func() {
					dialog.ShowError(fmt.Errorf("Failed to connect to %s: %v", m.apiBaseURL, err), m.window)
				})
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusCreated {
				data, _ := io.ReadAll(resp.Body)
				fyne.Do(func() {
					dialog.ShowError(fmt.Errorf("Registration failed: %s", string(data)), m.window)
				})
				return
			}

			var authRes struct {
				Success bool `json:"success"`
				Data    struct {
					Token string         `json:"token"`
					User  *database.User `json:"user"`
				} `json:"data"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&authRes); err == nil && authRes.Data.User != nil {
				fyne.Do(func() {
					m.authToken = authRes.Data.Token
					m.currentUser = authRes.Data.User

					m.userStatusLabel.SetText(fmt.Sprintf("Logged in: %s (%s)", m.currentUser.Username, strings.ToUpper(string(m.currentUser.Role))))
					m.loginBtn.Hide()
					m.registerBtn.Hide()
					m.logoutBtn.Show()

					m.refreshProfileView()
					m.refreshDeviceList()
					m.refreshUserFiles()
					if m.currentUser.Role == database.RoleAdmin {
						m.refreshUsersList()
					}
					dialog.ShowInformation("Welcome", fmt.Sprintf("Account created successfully for %s!", m.currentUser.Username), m.window)
				})
			}
		}()
	})
}

func (m *ManagerApp) logout() {
	m.authToken = ""
	m.currentUser = nil
	m.userStatusLabel.SetText("Status: Logged Out (Public Guest)")
	m.loginBtn.Show()
	m.registerBtn.Show()
	m.logoutBtn.Hide()
	m.profileInfoLabel.SetText("Please log in to view your profile and credentials.")
	m.apiKeyEntry.SetText("")
	m.filesListContainer.Objects = nil
	m.filesListContainer.Refresh()
	m.storageUsageLabel.SetText("Storage: Not logged in")
	if m.usersTableContainer != nil {
		m.usersTableContainer.Objects = nil
		m.usersTableContainer.Refresh()
	}
	dialog.ShowInformation("Logged Out", "You have been logged out successfully.", m.window)
}

// ==============================================================================
// My Profile & Personal API Key
// ==============================================================================

func (m *ManagerApp) buildProfileTab() fyne.CanvasObject {
	m.profileInfoLabel = widget.NewLabel("Please log in using the 'Log In' button in the top right.")
	m.profileInfoLabel.Wrapping = fyne.TextWrapWord

	m.apiKeyEntry = widget.NewEntry()
	m.apiKeyEntry.SetPlaceHolder("Your API key will appear here after logging in")

	copyKeyBtn := widget.NewButtonWithIcon("Copy API Key", theme.ContentCopyIcon(), func() {
		if m.apiKeyEntry.Text != "" {
			m.window.Clipboard().SetContent(m.apiKeyEntry.Text)
			dialog.ShowInformation("Copied", "API key copied to clipboard!", m.window)
		}
	})

	regenKeyBtn := widget.NewButtonWithIcon("Regenerate Key", theme.ViewRefreshIcon(), func() {
		m.regenerateMyAPIKey()
	})

	keyRow := container.NewBorder(nil, nil, nil, container.NewHBox(copyKeyBtn, regenKeyBtn), m.apiKeyEntry)

	changePassBtn := widget.NewButtonWithIcon("Change My Password", theme.DocumentCreateIcon(), func() {
		m.showChangePasswordDialog()
	})

	box := container.NewVBox(
		widget.NewLabelWithStyle("User Profile & Authentication", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		m.profileInfoLabel,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Personal API Key (for CLI, Python scripts, & Display devices):", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		keyRow,
		widget.NewSeparator(),
		container.NewHBox(changePassBtn),
	)

	return container.NewScroll(container.NewPadded(box))
}

func (m *ManagerApp) refreshProfileView() {
	if m.currentUser == nil {
		return
	}
	quotaMB := float64(m.currentUser.QuotaBytes) / (1024 * 1024)
	usedMB := float64(m.currentUser.StorageUsedBytes) / (1024 * 1024)
	percent := 0.0
	if quotaMB > 0 {
		percent = (usedMB / quotaMB) * 100.0
	}
	info := fmt.Sprintf("Username: %s\nEmail: %s\nRole: %s\nStorage Quota: %.2f MB used of %.2f MB (%.1f%%)\nUser ID: %d\nAccount Created: %s",
		m.currentUser.Username, m.currentUser.Email, m.currentUser.Role,
		usedMB, quotaMB, percent, m.currentUser.ID, m.currentUser.CreatedAt.Format(time.RFC822))
	m.profileInfoLabel.SetText(info)
	m.apiKeyEntry.SetText(m.currentUser.APIKey)
}

func (m *ManagerApp) regenerateMyAPIKey() {
	if m.authToken == "" {
		dialog.ShowInformation("Login Required", "Please log in first.", m.window)
		return
	}
	dialog.ShowConfirm("Regenerate API Key", "Are you sure? Any external device or script using your existing key will lose access immediately.", func(confirmed bool) {
		if !confirmed {
			return
		}
		go func() {
			req, _ := http.NewRequest("POST", m.apiBaseURL+"/api/v1/user/apikey/regenerate", nil)
			req.Header.Set("Authorization", "Bearer "+m.authToken)
			resp, err := m.httpClient.Do(req)
			if err != nil {
				fyne.Do(func() {
					dialog.ShowError(err, m.window)
				})
				return
			}
			defer resp.Body.Close()

			var res struct {
				Data struct {
					APIKey string `json:"api_key"`
				} `json:"data"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&res); err == nil && res.Data.APIKey != "" {
				fyne.Do(func() {
					m.currentUser.APIKey = res.Data.APIKey
					m.apiKeyEntry.SetText(res.Data.APIKey)
					dialog.ShowInformation("Key Regenerated", "Your new API key has been created and updated!", m.window)
				})
			}
		}()
	}, m.window)
}

func (m *ManagerApp) showChangePasswordDialog() {
	if m.authToken == "" {
		dialog.ShowInformation("Login Required", "Please log in first.", m.window)
		return
	}
	oldPassEntry := widget.NewPasswordEntry()
	newPassEntry := widget.NewPasswordEntry()
	confirmPassEntry := widget.NewPasswordEntry()

	form := widget.NewForm(
		widget.NewFormItem("Current Password", oldPassEntry),
		widget.NewFormItem("New Password", newPassEntry),
		widget.NewFormItem("Confirm Password", confirmPassEntry),
	)

	showWideDialog("Change Password", "Update", "Cancel", form, 540, 240, m.window, func(confirmed bool) {
		if !confirmed {
			return
		}
		if newPassEntry.Text != confirmPassEntry.Text {
			dialog.ShowError(fmt.Errorf("New passwords do not match"), m.window)
			return
		}
		if len(newPassEntry.Text) < 8 {
			dialog.ShowError(fmt.Errorf("New password must be at least 8 characters"), m.window)
			return
		}
		go func() {
			payload := map[string]string{
				"old_password": oldPassEntry.Text,
				"new_password": newPassEntry.Text,
			}
			b, _ := json.Marshal(payload)
			req, _ := http.NewRequest("PUT", m.apiBaseURL+"/api/v1/user/profile", bytes.NewReader(b))
			req.Header.Set("Authorization", "Bearer "+m.authToken)
			req.Header.Set("Content-Type", "application/json")
			resp, err := m.httpClient.Do(req)
			if err != nil {
				fyne.Do(func() {
					dialog.ShowError(err, m.window)
				})
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				fyne.Do(func() {
					dialog.ShowInformation("Success", "Password updated successfully!", m.window)
				})
			} else {
				data, _ := io.ReadAll(resp.Body)
				fyne.Do(func() {
					dialog.ShowError(fmt.Errorf("Update failed: %s", string(data)), m.window)
				})
			}
		}()
	})
}

// ==============================================================================
// My Devices (reTerminal E1001 / OpenDisplay) - Multiple Calendars Support
// ==============================================================================

func (m *ManagerApp) buildDeviceTab() fyne.CanvasObject {
	m.displayDeviceIDEntry = widget.NewEntry()
	m.displayDeviceIDEntry.SetText("reterminal-01")

	m.displayCityEntry = widget.NewEntry()
	m.displayCityEntry.SetText("New York, NY")

	m.displayLatEntry = widget.NewEntry()
	m.displayLatEntry.SetText("40.7128")

	m.displayLonEntry = widget.NewEntry()
	m.displayLonEntry.SetText("-74.0060")

	m.displayTimezoneEntry = widget.NewEntry()
	m.displayTimezoneEntry.SetText("America/New_York")

	// Multi-calendar entry with multiple rows
	m.displayCalURLEntry = widget.NewMultiLineEntry()
	m.displayCalURLEntry.SetMinRowsVisible(4)
	m.displayCalURLEntry.SetPlaceHolder("Multiple iCal (.ics) URLs (one per line, comma or semicolon separated):\nhttps://calendar.google.com/calendar/ical/.../basic.ics\nhttps://outlook.office365.com/.../reachcalendar.ics")

	m.displayFullRefreshEntry = widget.NewEntry()
	m.displayFullRefreshEntry.SetText("30")

	m.displayPartialRefreshEntry = widget.NewEntry()
	m.displayPartialRefreshEntry.SetText("1")

	m.displayUrlLabel = widget.NewLabel("reTerminal OpenDisplay URL: " + m.apiBaseURL + "/api/v1/display/reterminal-01/image.png")
	m.displayUrlLabel.Wrapping = fyne.TextWrapWord

	m.displayDeviceIDEntry.OnChanged = func(s string) {
		if m.displayUrlLabel != nil {
			m.displayUrlLabel.SetText("reTerminal OpenDisplay URL: " + m.apiBaseURL + "/api/v1/display/" + s + "/image.png")
		}
	}

	m.deviceSelect = widget.NewSelect([]string{"reterminal-01"}, func(s string) {
		if s != "" && s != "(New Device)" && m.displayDeviceIDEntry != nil {
			m.displayDeviceIDEntry.SetText(s)
			m.fetchDisplayConfigFor(s)
		}
	})
	m.deviceSelect.SetSelected("reterminal-01")

	saveBtn := widget.NewButtonWithIcon("Save Device Config", theme.DocumentSaveIcon(), func() {
		m.saveDisplayDevice()
	})
	saveBtn.Importance = widget.HighImportance

	deleteDeviceBtn := widget.NewButtonWithIcon("Delete Device", theme.DeleteIcon(), func() {
		m.deleteCurrentDevice()
	})
	deleteDeviceBtn.Importance = widget.DangerImportance

	refreshDevicesBtn := widget.NewButtonWithIcon("Refresh List", theme.ViewRefreshIcon(), func() {
		m.refreshDeviceList()
	})

	previewBtn := widget.NewButtonWithIcon("Preview 800x480 e-Paper", theme.MediaPlayIcon(), func() {
		m.previewDisplay()
	})

	addEventBtn := widget.NewButtonWithIcon("Add Quick Event", theme.ContentAddIcon(), func() {
		m.showAddEventDialog()
	})

	calendarHelpLabel := widget.NewLabelWithStyle("Tip: You can add multiple iCal calendars (personal, work, holidays). All feeds will be merged and chronologically sorted.", fyne.TextAlignLeading, fyne.TextStyle{Italic: true})

	form := widget.NewForm(
		widget.NewFormItem("Device Identifier", m.displayDeviceIDEntry),
		widget.NewFormItem("City Name", m.displayCityEntry),
		widget.NewFormItem("Latitude", m.displayLatEntry),
		widget.NewFormItem("Longitude", m.displayLonEntry),
		widget.NewFormItem("Timezone", m.displayTimezoneEntry),
		widget.NewFormItem("iCal Calendars (Multiple URLs)", m.displayCalURLEntry),
		widget.NewFormItem("Full Refresh (minutes)", m.displayFullRefreshEntry),
		widget.NewFormItem("Partial Refresh (minutes)", m.displayPartialRefreshEntry),
	)

	topSelectorRow := container.NewHBox(
		widget.NewLabelWithStyle("My Devices:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		m.deviceSelect,
		refreshDevicesBtn,
		deleteDeviceBtn,
	)

	content := container.NewVBox(
		topSelectorRow,
		widget.NewSeparator(),
		calendarHelpLabel,
		form,
		container.NewHBox(saveBtn, previewBtn, addEventBtn),
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Device Integration Helper:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		m.displayUrlLabel,
	)

	return container.NewScroll(container.NewPadded(content))
}

func (m *ManagerApp) refreshDeviceList() {
	if m.authToken == "" {
		return
	}
	go func() {
		req, _ := http.NewRequest("GET", m.apiBaseURL+"/api/v1/user/devices", nil)
		req.Header.Set("Authorization", "Bearer "+m.authToken)
		resp, err := m.httpClient.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()

		var res struct {
			Data []*database.DisplayConfig `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err == nil && len(res.Data) > 0 {
			var opts []string
			for _, d := range res.Data {
				opts = append(opts, d.DeviceID)
			}
			opts = append(opts, "(New Device)")
			fyne.Do(func() {
				m.deviceSelect.Options = opts
				m.deviceSelect.Refresh()
			})
		}
	}()
}

func (m *ManagerApp) fetchDisplayConfigFor(deviceID string) {
	go func() {
		url := fmt.Sprintf("%s/api/v1/display/%s/config", m.apiBaseURL, deviceID)
		req, _ := http.NewRequest("GET", url, nil)
		if m.authToken != "" {
			req.Header.Set("Authorization", "Bearer "+m.authToken)
		}
		resp, err := m.httpClient.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()

		var res struct {
			Data *database.DisplayConfig `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err == nil && res.Data != nil {
			fyne.Do(func() {
				m.displayDeviceIDEntry.SetText(res.Data.DeviceID)
				m.displayCityEntry.SetText(res.Data.CityName)
				m.displayLatEntry.SetText(fmt.Sprintf("%.4f", res.Data.Latitude))
				m.displayLonEntry.SetText(fmt.Sprintf("%.4f", res.Data.Longitude))
				m.displayTimezoneEntry.SetText(res.Data.Timezone)
				m.displayCalURLEntry.SetText(res.Data.CalendarURL)
				m.displayFullRefreshEntry.SetText(fmt.Sprintf("%d", res.Data.FullRefreshMinutes))
				m.displayPartialRefreshEntry.SetText(fmt.Sprintf("%d", res.Data.PartialRefreshMinutes))
			})
		}
	}()
}

func (m *ManagerApp) saveDisplayDevice() {
	if m.authToken == "" {
		dialog.ShowInformation("Login Required", "Please log in to register and save devices to your account.", m.window)
		return
	}
	lat, _ := strconv.ParseFloat(m.displayLatEntry.Text, 64)
	lon, _ := strconv.ParseFloat(m.displayLonEntry.Text, 64)
	fullRef, _ := strconv.Atoi(m.displayFullRefreshEntry.Text)
	partRef, _ := strconv.Atoi(m.displayPartialRefreshEntry.Text)

	payload := map[string]interface{}{
		"device_id":               strings.TrimSpace(m.displayDeviceIDEntry.Text),
		"city_name":               m.displayCityEntry.Text,
		"latitude":                lat,
		"longitude":               lon,
		"timezone":                m.displayTimezoneEntry.Text,
		"calendar_url":            m.displayCalURLEntry.Text,
		"full_refresh_minutes":    fullRef,
		"partial_refresh_minutes": partRef,
	}

	go func() {
		b, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", m.apiBaseURL+"/api/v1/user/devices", bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+m.authToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := m.httpClient.Do(req)
		if err != nil {
			fyne.Do(func() {
				dialog.ShowError(err, m.window)
			})
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			fyne.Do(func() {
				dialog.ShowInformation("Device Saved", fmt.Sprintf("Device %q successfully registered to your account!", m.displayDeviceIDEntry.Text), m.window)
				m.refreshDeviceList()
			})
		} else {
			data, _ := io.ReadAll(resp.Body)
			fyne.Do(func() {
				dialog.ShowError(fmt.Errorf("Failed to save device: %s", string(data)), m.window)
			})
		}
	}()
}

func (m *ManagerApp) deleteCurrentDevice() {
	if m.authToken == "" {
		dialog.ShowInformation("Login Required", "Please log in first.", m.window)
		return
	}
	devID := strings.TrimSpace(m.displayDeviceIDEntry.Text)
	if devID == "" || devID == "(New Device)" {
		return
	}
	dialog.ShowConfirm("Delete Device", fmt.Sprintf("Are you sure you want to delete device %q?", devID), func(confirmed bool) {
		if !confirmed {
			return
		}
		go func() {
			req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/api/v1/user/devices/%s", m.apiBaseURL, devID), nil)
			req.Header.Set("Authorization", "Bearer "+m.authToken)
			resp, err := m.httpClient.Do(req)
			if err != nil {
				fyne.Do(func() {
					dialog.ShowError(err, m.window)
				})
				return
			}
			defer resp.Body.Close()
			fyne.Do(func() {
				dialog.ShowInformation("Deleted", fmt.Sprintf("Device %q removed.", devID), m.window)
				m.refreshDeviceList()
			})
		}()
	}, m.window)
}

func (m *ManagerApp) previewDisplay() {
	go func() {
		url := fmt.Sprintf("%s/api/v1/display/render?device_id=%s", m.apiBaseURL, m.displayDeviceIDEntry.Text)
		req, _ := http.NewRequest("GET", url, nil)
		if m.authToken != "" {
			req.Header.Set("Authorization", "Bearer "+m.authToken)
		}
		resp, err := m.httpClient.Do(req)
		if err != nil {
			fyne.Do(func() {
				dialog.ShowError(fmt.Errorf("Failed to render preview: %v", err), m.window)
			})
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			data, _ := io.ReadAll(resp.Body)
			fyne.Do(func() {
				dialog.ShowError(fmt.Errorf("Render failed (%d): %s", resp.StatusCode, string(data)), m.window)
			})
			return
		}

		img, _, err := image.Decode(resp.Body)
		if err != nil {
			fyne.Do(func() {
				dialog.ShowError(fmt.Errorf("Failed to decode PNG image: %v", err), m.window)
			})
			return
		}

		refreshType := resp.Header.Get("X-Refresh-Type")
		deviceID := m.displayDeviceIDEntry.Text

		fyne.Do(func() {
			previewWin := fyne.CurrentApp().NewWindow(fmt.Sprintf("reTerminal Preview (800x480) - %s", deviceID))
			canvasImg := canvas.NewImageFromImage(img)
			canvasImg.FillMode = canvas.ImageFillContain
			canvasImg.SetMinSize(fyne.NewSize(800, 480))

			infoLabel := widget.NewLabel(fmt.Sprintf("Resolution: 800x480 Monochrome | Refresh Mode: %s", strings.ToUpper(refreshType)))

			previewWin.SetContent(container.NewBorder(nil, infoLabel, nil, nil, canvasImg))
			previewWin.Resize(fyne.NewSize(820, 520))
			previewWin.Show()
		})
	}()
}

func (m *ManagerApp) showAddEventDialog() {
	titleEntry := widget.NewEntry()
	titleEntry.SetPlaceHolder("Project Review / Meeting")
	locEntry := widget.NewEntry()
	locEntry.SetPlaceHolder("Office / Zoom")
	startEntry := widget.NewEntry()
	startEntry.SetText(time.Now().Add(1 * time.Hour).Format("15:04"))
	endEntry := widget.NewEntry()
	endEntry.SetText(time.Now().Add(2 * time.Hour).Format("15:04"))

	form := widget.NewForm(
		widget.NewFormItem("Event Title", titleEntry),
		widget.NewFormItem("Location", locEntry),
		widget.NewFormItem("Start Time (HH:MM)", startEntry),
		widget.NewFormItem("End Time (HH:MM)", endEntry),
	)

	showWideDialog("Add Calendar Event to reTerminal", "Add Event", "Cancel", form, 540, 260, m.window, func(confirmed bool) {
		if !confirmed || titleEntry.Text == "" {
			return
		}
		now := time.Now()
		st, _ := time.Parse("15:04", startEntry.Text)
		et, _ := time.Parse("15:04", endEntry.Text)
		startTime := time.Date(now.Year(), now.Month(), now.Day(), st.Hour(), st.Minute(), 0, 0, time.Local)
		endTime := time.Date(now.Year(), now.Month(), now.Day(), et.Hour(), et.Minute(), 0, 0, time.Local)

		payload := map[string]interface{}{
			"device_id":  m.displayDeviceIDEntry.Text,
			"title":      titleEntry.Text,
			"location":   locEntry.Text,
			"start_time": startTime.Format(time.RFC3339),
			"end_time":   endTime.Format(time.RFC3339),
		}

		go func() {
			b, _ := json.Marshal(payload)
			req, _ := http.NewRequest("POST", m.apiBaseURL+"/api/v1/display/events", bytes.NewReader(b))
			if m.authToken != "" {
				req.Header.Set("Authorization", "Bearer "+m.authToken)
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := m.httpClient.Do(req)
			if err != nil {
				fyne.Do(func() {
					dialog.ShowError(err, m.window)
				})
				return
			}
			defer resp.Body.Close()
			fyne.Do(func() {
				dialog.ShowInformation("Event Added", "Event added! Next reTerminal refresh will display it.", m.window)
			})
		}()
	})
}

// ==============================================================================
// My Cloud Files & Data Management
// ==============================================================================

func (m *ManagerApp) buildUserDataTab() fyne.CanvasObject {
	m.storageUsageLabel = widget.NewLabel("Storage Usage: Log in to inspect your cloud storage.")
	m.filesListContainer = container.NewVBox()

	refreshBtn := widget.NewButtonWithIcon("Refresh Files", theme.ViewRefreshIcon(), func() {
		m.refreshUserFiles()
	})

	uploadBtn := widget.NewButtonWithIcon("Upload File to NAS", theme.UploadIcon(), func() {
		m.uploadFileToCloud()
	})
	uploadBtn.Importance = widget.HighImportance

	header := container.NewBorder(
		nil, nil,
		m.storageUsageLabel,
		container.NewHBox(uploadBtn, refreshBtn),
	)

	return container.NewBorder(
		container.NewVBox(container.NewPadded(header), widget.NewSeparator()),
		nil, nil, nil,
		container.NewScroll(m.filesListContainer),
	)
}

func (m *ManagerApp) refreshUserFiles() {
	if m.authToken == "" {
		fyne.Do(func() {
			m.storageUsageLabel.SetText("Storage: Not logged in")
			m.filesListContainer.Objects = nil
			m.filesListContainer.Refresh()
		})
		return
	}

	go func() {
		// 1. Fetch quota
		reqQuota, _ := http.NewRequest("GET", m.apiBaseURL+"/api/v1/storage/quota", nil)
		reqQuota.Header.Set("Authorization", "Bearer "+m.authToken)
		if resp, err := m.httpClient.Do(reqQuota); err == nil {
			var qRes struct {
				Data struct {
					UsedBytes  int64   `json:"used_bytes"`
					QuotaBytes int64   `json:"quota_bytes"`
					Percentage float64 `json:"percentage"`
				} `json:"data"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&qRes); err == nil {
				fyne.Do(func() {
					m.storageUsageLabel.SetText(fmt.Sprintf("Storage: %.2f MB / %.2f GB (%.1f%% used)",
						float64(qRes.Data.UsedBytes)/(1024*1024),
						float64(qRes.Data.QuotaBytes)/(1024*1024*1024),
						qRes.Data.Percentage))
				})
			}
			resp.Body.Close()
		}

		// 2. Fetch file list
		reqFiles, _ := http.NewRequest("GET", m.apiBaseURL+"/api/v1/storage/files", nil)
		reqFiles.Header.Set("Authorization", "Bearer "+m.authToken)
		respFiles, err := m.httpClient.Do(reqFiles)
		if err != nil {
			return
		}
		defer respFiles.Body.Close()

		var res struct {
			Data []struct {
				Name    string    `json:"name"`
				Size    int64     `json:"size"`
				ModTime time.Time `json:"mod_time"`
				IsDir   bool      `json:"is_dir"`
			} `json:"data"`
		}
		if err := json.NewDecoder(respFiles.Body).Decode(&res); err != nil {
			return
		}

		var items []fyne.CanvasObject
		if len(res.Data) == 0 {
			items = append(items, widget.NewLabel("No files uploaded yet. Click 'Upload File to NAS' to store data on your PocketCloud NAS!"))
		} else {
			for _, file := range res.Data {
				fileName := file.Name
				fileSizeKB := float64(file.Size) / 1024.0

				label := widget.NewLabel(fmt.Sprintf("📄 %s (%.1f KB) - %s", fileName, fileSizeKB, file.ModTime.Format("2006-01-02 15:04")))

				downloadBtn := widget.NewButtonWithIcon("Download", theme.DownloadIcon(), func() {
					m.downloadCloudFile(fileName)
				})

				deleteBtn := widget.NewButtonWithIcon("Delete", theme.DeleteIcon(), func() {
					m.deleteCloudFile(fileName)
				})
				deleteBtn.Importance = widget.DangerImportance

				row := container.NewBorder(nil, nil, label, container.NewHBox(downloadBtn, deleteBtn))
				items = append(items, row, widget.NewSeparator())
			}
		}

		fyne.Do(func() {
			m.filesListContainer.Objects = items
			m.filesListContainer.Refresh()
		})
	}()
}

func (m *ManagerApp) uploadFileToCloud() {
	if m.authToken == "" {
		dialog.ShowInformation("Login Required", "Please log in to upload files.", m.window)
		return
	}

	dialog.ShowFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil || reader == nil {
			return
		}
		defer reader.Close()

		data, err := io.ReadAll(reader)
		if err != nil {
			fyne.Do(func() {
				dialog.ShowError(err, m.window)
			})
			return
		}

		fileName := reader.URI().Name()

		go func() {
			var b bytes.Buffer
			w := multipart.NewWriter(&b)
			part, err := w.CreateFormFile("file", fileName)
			if err != nil {
				fyne.Do(func() {
					dialog.ShowError(err, m.window)
				})
				return
			}
			_, _ = part.Write(data)
			_ = w.Close()

			req, _ := http.NewRequest("POST", m.apiBaseURL+"/api/v1/storage/upload", &b)
			req.Header.Set("Authorization", "Bearer "+m.authToken)
			req.Header.Set("Content-Type", w.FormDataContentType())

			resp, err := m.httpClient.Do(req)
			if err != nil {
				fyne.Do(func() {
					dialog.ShowError(err, m.window)
				})
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusCreated {
				fyne.Do(func() {
					dialog.ShowInformation("Uploaded", fmt.Sprintf("Successfully uploaded %s to NAS!", fileName), m.window)
					m.refreshUserFiles()
				})
			} else {
				resBody, _ := io.ReadAll(resp.Body)
				fyne.Do(func() {
					dialog.ShowError(fmt.Errorf("Upload failed: %s", string(resBody)), m.window)
				})
			}
		}()
	}, m.window)
}

func (m *ManagerApp) downloadCloudFile(fileName string) {
	dialog.ShowFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil || writer == nil {
			return
		}
		defer writer.Close()

		go func() {
			url := fmt.Sprintf("%s/api/v1/storage/download?path=%s", m.apiBaseURL, fileName)
			req, _ := http.NewRequest("GET", url, nil)
			req.Header.Set("Authorization", "Bearer "+m.authToken)
			resp, err := m.httpClient.Do(req)
			if err != nil {
				fyne.Do(func() {
					dialog.ShowError(err, m.window)
				})
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				fyne.Do(func() {
					dialog.ShowError(fmt.Errorf("Download failed with status %d", resp.StatusCode), m.window)
				})
				return
			}

			_, _ = io.Copy(writer, resp.Body)
			fyne.Do(func() {
				dialog.ShowInformation("Downloaded", fmt.Sprintf("File %s downloaded successfully!", fileName), m.window)
			})
		}()
	}, m.window)
}

func (m *ManagerApp) deleteCloudFile(fileName string) {
	dialog.ShowConfirm("Delete File", fmt.Sprintf("Are you sure you want to delete %q from your cloud storage?", fileName), func(confirmed bool) {
		if !confirmed {
			return
		}
		go func() {
			url := fmt.Sprintf("%s/api/v1/storage/files?path=%s", m.apiBaseURL, fileName)
			req, _ := http.NewRequest("DELETE", url, nil)
			req.Header.Set("Authorization", "Bearer "+m.authToken)
			resp, err := m.httpClient.Do(req)
			if err != nil {
				fyne.Do(func() {
					dialog.ShowError(err, m.window)
				})
				return
			}
			defer resp.Body.Close()
			fyne.Do(func() {
				dialog.ShowInformation("Deleted", fmt.Sprintf("File %s deleted.", fileName), m.window)
				m.refreshUserFiles()
			})
		}()
	}, m.window)
}

// ==============================================================================
// Admin: User Management (Create, Edit, Delete, API Key)
// ==============================================================================

func (m *ManagerApp) buildUsersTab() fyne.CanvasObject {
	m.usersTableContainer = container.NewVBox()

	refreshBtn := widget.NewButtonWithIcon("Refresh Users", theme.ViewRefreshIcon(), func() {
		m.refreshUsersList()
	})

	createUserBtn := widget.NewButtonWithIcon("Create New User", theme.ContentAddIcon(), func() {
		m.showCreateUserDialog()
	})
	createUserBtn.Importance = widget.HighImportance

	header := container.NewBorder(
		nil, nil,
		widget.NewLabelWithStyle("Admin User Management (Create, Edit, Delete, API Keys)", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewHBox(createUserBtn, refreshBtn),
	)

	return container.NewBorder(
		container.NewVBox(container.NewPadded(header), widget.NewSeparator()),
		nil, nil, nil,
		container.NewScroll(m.usersTableContainer),
	)
}

func (m *ManagerApp) refreshUsersList() {
	if m.authToken == "" {
		fyne.Do(func() {
			m.usersTableContainer.Objects = []fyne.CanvasObject{widget.NewLabel("Please log in as an administrator to view and manage users.")}
			m.usersTableContainer.Refresh()
		})
		return
	}

	go func() {
		req, _ := http.NewRequest("GET", m.apiBaseURL+"/api/v1/admin/users", nil)
		req.Header.Set("Authorization", "Bearer "+m.authToken)
		resp, err := m.httpClient.Do(req)
		if err != nil {
			fyne.Do(func() {
				dialog.ShowError(err, m.window)
			})
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			fyne.Do(func() {
				m.usersTableContainer.Objects = []fyne.CanvasObject{widget.NewLabel("Access Denied: Current user does not have Administrator privileges.")}
				m.usersTableContainer.Refresh()
			})
			return
		}

		var res struct {
			Data []*database.User `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
			return
		}

		var rows []fyne.CanvasObject
		for _, u := range res.Data {
			userCopy := *u
			cardHeader := fmt.Sprintf("👤 %s (%s)", userCopy.Username, strings.ToUpper(string(userCopy.Role)))
			quotaGB := float64(userCopy.QuotaBytes) / (1024 * 1024 * 1024)
			usedMB := float64(userCopy.StorageUsedBytes) / (1024 * 1024)

			stats := widget.NewLabel(fmt.Sprintf("Email: %s | Quota: %.1f GB | Used: %.2f MB\nAPI Key: %s",
				userCopy.Email, quotaGB, usedMB, userCopy.APIKey))

			editBtn := widget.NewButtonWithIcon("Edit", theme.DocumentCreateIcon(), func() {
				m.showEditUserDialog(&userCopy)
			})
			deleteBtn := widget.NewButtonWithIcon("Delete", theme.DeleteIcon(), func() {
				m.confirmDeleteUser(&userCopy)
			})
			deleteBtn.Importance = widget.DangerImportance

			copyKeyBtn := widget.NewButtonWithIcon("Copy Key", theme.ContentCopyIcon(), func() {
				m.window.Clipboard().SetContent(userCopy.APIKey)
				dialog.ShowInformation("Copied", fmt.Sprintf("Copied API key for user %s", userCopy.Username), m.window)
			})

			regenBtn := widget.NewButtonWithIcon("Regen Key", theme.ViewRefreshIcon(), func() {
				m.adminRegenKey(userCopy.ID)
			})

			actions := container.NewHBox(editBtn, copyKeyBtn, regenBtn, deleteBtn)
			userCard := widget.NewCard(cardHeader, "", container.NewVBox(stats, actions))
			rows = append(rows, userCard)
		}

		fyne.Do(func() {
			m.usersTableContainer.Objects = rows
			m.usersTableContainer.Refresh()
		})
	}()
}

func (m *ManagerApp) showCreateUserDialog() {
	if m.authToken == "" {
		dialog.ShowInformation("Not Logged In", "Please log in as admin first.", m.window)
		return
	}
	userEntry := widget.NewEntry()
	emailEntry := widget.NewEntry()
	passEntry := widget.NewPasswordEntry()
	apiKeyEntry := widget.NewEntry()
	apiKeyEntry.SetPlaceHolder("(Optional: leave blank to auto-generate secure 32-byte key)")

	roleSelect := widget.NewSelect([]string{"user", "admin"}, nil)
	roleSelect.SetSelected("user")
	quotaEntry := widget.NewEntry()
	quotaEntry.SetText("50")

	form := widget.NewForm(
		widget.NewFormItem("Username", userEntry),
		widget.NewFormItem("Email", emailEntry),
		widget.NewFormItem("Password", passEntry),
		widget.NewFormItem("Custom API Key", apiKeyEntry),
		widget.NewFormItem("Role", roleSelect),
		widget.NewFormItem("Quota (GB)", quotaEntry),
	)

	showWideDialog("Admin: Create New User", "Create User", "Cancel", form, 600, 360, m.window, func(confirmed bool) {
		if !confirmed || userEntry.Text == "" || passEntry.Text == "" {
			return
		}
		qGB, _ := strconv.ParseInt(quotaEntry.Text, 10, 64)
		if qGB <= 0 {
			qGB = 50
		}
		payload := map[string]interface{}{
			"username":    userEntry.Text,
			"email":       emailEntry.Text,
			"password":    passEntry.Text,
			"api_key":     strings.TrimSpace(apiKeyEntry.Text),
			"role":        roleSelect.Selected,
			"quota_bytes": qGB * 1024 * 1024 * 1024,
		}
		go func() {
			b, _ := json.Marshal(payload)
			req, _ := http.NewRequest("POST", m.apiBaseURL+"/api/v1/admin/users", bytes.NewReader(b))
			req.Header.Set("Authorization", "Bearer "+m.authToken)
			req.Header.Set("Content-Type", "application/json")
			resp, err := m.httpClient.Do(req)
			if err != nil {
				fyne.Do(func() {
					dialog.ShowError(err, m.window)
				})
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusCreated {
				fyne.Do(func() {
					dialog.ShowInformation("User Created", fmt.Sprintf("User %q created successfully!", userEntry.Text), m.window)
					m.refreshUsersList()
				})
			} else {
				data, _ := io.ReadAll(resp.Body)
				fyne.Do(func() {
					dialog.ShowError(fmt.Errorf("Failed: %s", string(data)), m.window)
				})
			}
		}()
	})
}

func (m *ManagerApp) showEditUserDialog(u *database.User) {
	usernameEntry := widget.NewEntry()
	usernameEntry.SetText(u.Username)
	emailEntry := widget.NewEntry()
	emailEntry.SetText(u.Email)
	passEntry := widget.NewPasswordEntry()
	passEntry.SetPlaceHolder("(Leave empty to keep existing password)")
	apiKeyEntry := widget.NewEntry()
	apiKeyEntry.SetText(u.APIKey)
	roleSelect := widget.NewSelect([]string{"user", "admin"}, nil)
	roleSelect.SetSelected(string(u.Role))
	quotaEntry := widget.NewEntry()
	quotaEntry.SetText(fmt.Sprintf("%d", u.QuotaBytes/(1024*1024*1024)))

	form := widget.NewForm(
		widget.NewFormItem("Username", usernameEntry),
		widget.NewFormItem("Email", emailEntry),
		widget.NewFormItem("New Password", passEntry),
		widget.NewFormItem("API Key", apiKeyEntry),
		widget.NewFormItem("Role", roleSelect),
		widget.NewFormItem("Quota (GB)", quotaEntry),
	)

	showWideDialog("Admin: Edit User "+u.Username, "Save Changes", "Cancel", form, 600, 360, m.window, func(confirmed bool) {
		if !confirmed {
			return
		}
		qGB, _ := strconv.ParseInt(quotaEntry.Text, 10, 64)
		if qGB <= 0 {
			qGB = 50
		}
		payload := map[string]interface{}{
			"username":    strings.TrimSpace(usernameEntry.Text),
			"email":       emailEntry.Text,
			"role":        roleSelect.Selected,
			"quota_bytes": qGB * 1024 * 1024 * 1024,
			"api_key":     strings.TrimSpace(apiKeyEntry.Text),
			"password":    passEntry.Text,
		}
		go func() {
			b, _ := json.Marshal(payload)
			req, _ := http.NewRequest("PUT", fmt.Sprintf("%s/api/v1/admin/users/%d", m.apiBaseURL, u.ID), bytes.NewReader(b))
			req.Header.Set("Authorization", "Bearer "+m.authToken)
			req.Header.Set("Content-Type", "application/json")
			resp, err := m.httpClient.Do(req)
			if err != nil {
				fyne.Do(func() {
					dialog.ShowError(err, m.window)
				})
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				fyne.Do(func() {
					dialog.ShowInformation("Updated", "User details updated!", m.window)
					m.refreshUsersList()
				})
			} else {
				data, _ := io.ReadAll(resp.Body)
				fyne.Do(func() {
					dialog.ShowError(fmt.Errorf("Update failed: %s", string(data)), m.window)
				})
			}
		}()
	})
}

func (m *ManagerApp) confirmDeleteUser(u *database.User) {
	dialog.ShowConfirm("Delete User", fmt.Sprintf("Are you sure you want to delete user %q?\nThis will permanently remove their account, API key, and storage directory.", u.Username), func(confirmed bool) {
		if !confirmed {
			return
		}
		go func() {
			req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/api/v1/admin/users/%d", m.apiBaseURL, u.ID), nil)
			req.Header.Set("Authorization", "Bearer "+m.authToken)
			resp, err := m.httpClient.Do(req)
			if err != nil {
				fyne.Do(func() {
					dialog.ShowError(err, m.window)
				})
				return
			}
			defer resp.Body.Close()
			fyne.Do(func() {
				dialog.ShowInformation("Deleted", fmt.Sprintf("User %s has been deleted.", u.Username), m.window)
				m.refreshUsersList()
			})
		}()
	}, m.window)
}

func (m *ManagerApp) adminRegenKey(userID int64) {
	go func() {
		req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/admin/users/%d/apikey", m.apiBaseURL, userID), nil)
		req.Header.Set("Authorization", "Bearer "+m.authToken)
		resp, err := m.httpClient.Do(req)
		if err != nil {
			fyne.Do(func() {
				dialog.ShowError(err, m.window)
			})
			return
		}
		defer resp.Body.Close()

		var res struct {
			Data struct {
				APIKey string `json:"api_key"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err == nil {
			fyne.Do(func() {
				dialog.ShowInformation("New API Key", fmt.Sprintf("Key regenerated:\n%s", res.Data.APIKey), m.window)
				m.refreshUsersList()
			})
		}
	}()
}

// ==============================================================================
// Admin: System-Wide Settings & Health
// ==============================================================================

func (m *ManagerApp) buildSettingsTab() fyne.CanvasObject {
	m.mountPathEntry = widget.NewEntry()
	m.mountPathEntry.SetText("/mnt/pocketcloud/storage")
	m.settingsPortEntry = widget.NewEntry()
	m.settingsPortEntry.SetText("8080")
	m.quotaEntry = widget.NewEntry()
	m.quotaEntry.SetText("50")
	m.uploadEntry = widget.NewEntry()
	m.uploadEntry.SetText("2048")
	m.allowRegCheck = widget.NewCheck("Allow Public User Self-Registration", nil)
	m.allowRegCheck.SetChecked(true)
	m.healthStatusLabel = widget.NewLabel("Health: Click 'Fetch Server Settings & Health' to query system status.")
	m.healthStatusLabel.Wrapping = fyne.TextWrapWord

	fetchBtn := widget.NewButtonWithIcon("Fetch Server Settings & Health", theme.DownloadIcon(), func() {
		m.fetchAdminSettings()
	})

	saveBtn := widget.NewButtonWithIcon("Save System Settings", theme.DocumentSaveIcon(), func() {
		m.saveAdminSettings()
	})
	saveBtn.Importance = widget.HighImportance

	form := widget.NewForm(
		widget.NewFormItem("Server Port", m.settingsPortEntry),
		widget.NewFormItem("NAS Base Mount Path", m.mountPathEntry),
		widget.NewFormItem("Default Quota per User (GB)", m.quotaEntry),
		widget.NewFormItem("Max Upload File Size (MB)", m.uploadEntry),
		widget.NewFormItem("Public Registration", m.allowRegCheck),
	)

	content := container.NewVBox(
		widget.NewLabelWithStyle("System-Wide Server Configuration", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewHBox(fetchBtn, saveBtn),
		widget.NewSeparator(),
		form,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("System Health & NAS Hardware Status:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		m.healthStatusLabel,
	)

	return container.NewScroll(container.NewPadded(content))
}

func (m *ManagerApp) fetchAdminSettings() {
	if m.authToken == "" {
		dialog.ShowInformation("Login Required", "Please log in as admin.", m.window)
		return
	}
	go func() {
		// Fetch settings
		req, _ := http.NewRequest("GET", m.apiBaseURL+"/api/v1/admin/settings", nil)
		req.Header.Set("Authorization", "Bearer "+m.authToken)
		resp, err := m.httpClient.Do(req)
		if err != nil {
			fyne.Do(func() {
				dialog.ShowError(err, m.window)
			})
			return
		}
		defer resp.Body.Close()

		var res struct {
			Data config.Config `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err == nil {
			fyne.Do(func() {
				m.mountPathEntry.SetText(res.Data.Storage.BaseMountPath)
				m.settingsPortEntry.SetText(fmt.Sprintf("%d", res.Data.Server.Port))
				m.quotaEntry.SetText(fmt.Sprintf("%d", res.Data.Storage.DefaultQuotaBytes/(1024*1024*1024)))
				m.uploadEntry.SetText(fmt.Sprintf("%d", res.Data.Storage.MaxUploadSizeMB))
				m.allowRegCheck.SetChecked(res.Data.Auth.AllowRegistration)
			})
		}

		// Fetch health
		reqH, _ := http.NewRequest("GET", m.apiBaseURL+"/api/v1/admin/health", nil)
		reqH.Header.Set("Authorization", "Bearer "+m.authToken)
		if respH, err := m.httpClient.Do(reqH); err == nil {
			var hRes struct {
				Data struct {
					Status       string `json:"status"`
					Uptime       string `json:"uptime"`
					GoVersion    string `json:"go_version"`
					NumGoroutine int    `json:"num_goroutine"`
					Memory       struct {
						AllocMB int `json:"alloc_mb"`
						SysMB   int `json:"sys_mb"`
					} `json:"memory"`
				} `json:"data"`
			}
			if err := json.NewDecoder(respH.Body).Decode(&hRes); err == nil {
				fyne.Do(func() {
					m.healthStatusLabel.SetText(fmt.Sprintf("Status: %s | Uptime: %s | Goroutines: %d | Mem Alloc: %d MB (Sys: %d MB) | Runtime: %s",
						strings.ToUpper(hRes.Data.Status), hRes.Data.Uptime, hRes.Data.NumGoroutine, hRes.Data.Memory.AllocMB, hRes.Data.Memory.SysMB, hRes.Data.GoVersion))
				})
			}
			respH.Body.Close()
		}

		fyne.Do(func() {
			dialog.ShowInformation("Fetched", "Settings and system health retrieved successfully!", m.window)
		})
	}()
}

func (m *ManagerApp) saveAdminSettings() {
	if m.authToken == "" {
		dialog.ShowInformation("Login Required", "Please log in as admin.", m.window)
		return
	}
	port, _ := strconv.Atoi(m.settingsPortEntry.Text)
	quota, _ := strconv.ParseInt(m.quotaEntry.Text, 10, 64)
	upload, _ := strconv.ParseInt(m.uploadEntry.Text, 10, 64)

	payload := map[string]interface{}{
		"port":               port,
		"base_mount_path":    m.mountPathEntry.Text,
		"default_quota_gb":   quota,
		"max_upload_size_mb": upload,
		"allow_registration": m.allowRegCheck.Checked,
	}

	go func() {
		b, _ := json.Marshal(payload)
		req, _ := http.NewRequest("PUT", m.apiBaseURL+"/api/v1/admin/settings", bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+m.authToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := m.httpClient.Do(req)
		if err != nil {
			fyne.Do(func() {
				dialog.ShowError(err, m.window)
			})
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			fyne.Do(func() {
				dialog.ShowInformation("Saved", "System-wide settings updated successfully!", m.window)
			})
		} else {
			data, _ := io.ReadAll(resp.Body)
			fyne.Do(func() {
				dialog.ShowError(fmt.Errorf("Update failed: %s", string(data)), m.window)
			})
		}
	}()
}

// ==============================================================================
// SSH & Host Service Management
// ==============================================================================

func (m *ManagerApp) buildServiceTab() fyne.CanvasObject {
	m.hostEntry = widget.NewEntry()
	m.hostEntry.SetText("192.168.3.54")
	m.portEntry = widget.NewEntry()
	m.portEntry.SetText("22")
	m.userEntry = widget.NewEntry()
	m.userEntry.SetText("lab")
	m.passEntry = widget.NewPasswordEntry()
	m.passEntry.SetPlaceHolder("SSH password")
	m.keyEntry = widget.NewEntry()
	m.keyEntry.SetPlaceHolder("~/.ssh/id_rsa (optional)")
	m.connLabel = widget.NewLabel("SSH: Disconnected")

	connectBtn := widget.NewButtonWithIcon("Connect SSH", theme.LoginIcon(), func() {
		m.connectSSH()
	})

	m.serviceStatusLabel = widget.NewLabel("Systemd Service: Unknown")
	m.logsEntry = widget.NewMultiLineEntry()
	m.logsEntry.SetMinRowsVisible(14)

	refreshBtn := widget.NewButtonWithIcon("Refresh Status & Logs", theme.ViewRefreshIcon(), func() {
		m.refreshStatus()
	})
	startBtn := widget.NewButtonWithIcon("Start Service", theme.MediaPlayIcon(), func() {
		m.runServiceCmd("sudo systemctl start neonservices")
	})
	stopBtn := widget.NewButtonWithIcon("Stop Service", theme.MediaStopIcon(), func() {
		m.runServiceCmd("sudo systemctl stop neonservices")
	})
	restartBtn := widget.NewButtonWithIcon("Restart Service", theme.ViewRefreshIcon(), func() {
		m.runServiceCmd("sudo systemctl restart neonservices")
	})

	connBox := container.NewVBox(
		widget.NewLabelWithStyle("SSH Host Connection:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewGridWithColumns(5, m.hostEntry, m.portEntry, m.userEntry, m.passEntry, connectBtn),
		m.connLabel,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Remote neonservices.service Controls:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		m.serviceStatusLabel,
		container.NewHBox(refreshBtn, startBtn, restartBtn, stopBtn),
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Recent Journalctl Logs:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		m.logsEntry,
	)

	return container.NewScroll(container.NewPadded(connBox))
}

func (m *ManagerApp) connectSSH() {
	m.connLabel.SetText("Connecting via SSH...")
	port, _ := strconv.Atoi(m.portEntry.Text)
	if port <= 0 {
		port = 22
	}
	go func() {
		client := sshutil.NewClient(sshutil.Options{
			Host:     m.hostEntry.Text,
			Port:     port,
			User:     m.userEntry.Text,
			Password: m.passEntry.Text,
			KeyPath:  m.keyEntry.Text,
			Timeout:  10 * time.Second,
		})
		if err := client.Connect(); err != nil {
			fyne.Do(func() {
				m.connLabel.SetText(fmt.Sprintf("SSH Error: %v", err))
				dialog.ShowError(err, m.window)
			})
			return
		}
		m.sshClient = client
		fyne.Do(func() {
			m.connLabel.SetText(fmt.Sprintf("SSH Connected to %s@%s:%d", m.userEntry.Text, m.hostEntry.Text, port))
			m.refreshStatus()
		})
	}()
}

func (m *ManagerApp) runServiceCmd(cmd string) {
	if m.sshClient == nil {
		dialog.ShowInformation("Not Connected", "Please connect to SSH first.", m.window)
		return
	}
	go func() {
		_, err := m.sshClient.Run(cmd)
		if err != nil {
			fyne.Do(func() {
				dialog.ShowError(err, m.window)
			})
		}
		time.Sleep(500 * time.Millisecond)
		m.refreshStatus()
	}()
}

func (m *ManagerApp) refreshStatus() {
	if m.sshClient == nil {
		return
	}
	go func() {
		statusOut, _ := m.sshClient.Run("systemctl is-active neonservices || true")
		status := strings.TrimSpace(statusOut)
		logs, err := m.sshClient.Run("journalctl -u neonservices -n 35 --no-pager 2>&1 || true")

		fyne.Do(func() {
			m.serviceStatusLabel.SetText(fmt.Sprintf("neonservices.service: %s", strings.ToUpper(status)))
			if err == nil {
				m.logsEntry.SetText(logs)
			}
		})
	}()
}

// ==============================================================================
// NAS Host Storage & Mount Tab
// ==============================================================================

func (m *ManagerApp) buildNASTab() fyne.CanvasObject {
	m.nasStatusText = widget.NewLabel("NAS Status: Not queried. Connect SSH and click below to inspect.")
	m.nasStatusText.Wrapping = fyne.TextWrapWord

	inspectBtn := widget.NewButtonWithIcon("Inspect Hardware NAS Mount", theme.SearchIcon(), func() {
		if m.sshClient == nil {
			dialog.ShowInformation("Not Connected", "Please connect to SSH in the SSH tab first.", m.window)
			return
		}
		go func() {
			cmd := "df -h /mnt/pocketcloud && ls -la /mnt/pocketcloud 2>&1 || true"
			out, err := m.sshClient.Run(cmd)
			fyne.Do(func() {
				if err != nil {
					m.nasStatusText.SetText(fmt.Sprintf("Failed or unmounted:\n%s\n%v", out, err))
				} else {
					m.nasStatusText.SetText(fmt.Sprintf("StationPC PocketCloud NAS Storage Status:\n\n%s", out))
				}
			})
		}()
	})

	content := container.NewVBox(
		widget.NewLabelWithStyle("StationPC PocketCloud NAS Host Mount (/mnt/pocketcloud):", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		inspectBtn,
		widget.NewSeparator(),
		m.nasStatusText,
	)

	return container.NewScroll(container.NewPadded(content))
}
