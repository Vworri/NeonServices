package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
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
	"gopkg.in/yaml.v3"

	"github.com/neonphnx/NeonServices/internal/config"
	"github.com/neonphnx/NeonServices/internal/sshutil"
)

type ManagerApp struct {
	window    fyne.Window
	sshClient *sshutil.Client

	// Connection inputs
	hostEntry *widget.Entry
	portEntry *widget.Entry
	userEntry *widget.Entry
	keyEntry  *widget.Entry
	passEntry *widget.PasswordEntry
	connLabel *widget.Label

	// Config inputs
	jwtSecretEntry *widget.Entry
	serverPortEntry *widget.Entry
	dbPathEntry    *widget.Entry
	nasMountEntry  *widget.Entry
	quotaGBEntry   *widget.Entry
	uploadMBEntry  *widget.Entry
	rawConfigEntry *widget.Entry

	// Service controls
	serviceStatusLabel *widget.Label
	logsEntry          *widget.Entry

	// NAS Monitor
	nasStatusText *widget.Label
}

func main() {
	a := app.NewWithID("com.neonservices.manager")
	w := a.NewWindow("NeonServices Control Center & Secrets Manager")
	w.Resize(fyne.NewSize(960, 680))

	m := &ManagerApp{window: w}
	w.SetContent(m.buildUI())
	w.ShowAndRun()
}

func (m *ManagerApp) buildUI() fyne.CanvasObject {
	tabs := container.NewAppTabs(
		container.NewTabItemWithIcon("SSH Connection", theme.ComputerIcon(), m.buildConnectionTab()),
		container.NewTabItemWithIcon("Secrets & Config", theme.DocumentCreateIcon(), m.buildConfigTab()),
		container.NewTabItemWithIcon("Service & Logs", theme.ViewRefreshIcon(), m.buildServiceTab()),
		container.NewTabItemWithIcon("NAS & Storage", theme.StorageIcon(), m.buildNASTab()),
	)
	tabs.SetTabLocation(container.TabLocationTop)

	header := container.NewHBox(
		canvas.NewText("  NEON SERVICES - Remote Manager", color.NRGBA{R: 0, G: 200, B: 255, A: 255}),
		widget.NewLabel(" | Ubuntu & StationPC PocketCloud Controller"),
	)

	return container.NewBorder(header, nil, nil, nil, tabs)
}

func (m *ManagerApp) buildConnectionTab() fyne.CanvasObject {
	m.hostEntry = widget.NewEntry()
	m.hostEntry.SetText("192.168.1.100")

	m.portEntry = widget.NewEntry()
	m.portEntry.SetText("22")

	m.userEntry = widget.NewEntry()
	m.userEntry.SetText("ubuntu")

	defaultKey := filepath.Join(os.Getenv("HOME"), ".ssh", "id_ed25519")
	if _, err := os.Stat(defaultKey); os.IsNotExist(err) {
		defaultKey = filepath.Join(os.Getenv("HOME"), ".ssh", "id_rsa")
	}
	m.keyEntry = widget.NewEntry()
	m.keyEntry.SetText(defaultKey)

	m.passEntry = widget.NewPasswordEntry()
	m.passEntry.SetPlaceHolder("(Optional if using SSH key)")

	m.connLabel = widget.NewLabel("Status: Disconnected")

	connectBtn := widget.NewButtonWithIcon("Connect to Server", theme.LoginIcon(), func() {
		m.connectSSH()
	})
	connectBtn.Importance = widget.HighImportance

	disconnectBtn := widget.NewButtonWithIcon("Disconnect", theme.LogoutIcon(), func() {
		if m.sshClient != nil {
			_ = m.sshClient.Close()
			m.sshClient = nil
			m.connLabel.SetText("Status: Disconnected")
			dialog.ShowInformation("Disconnected", "SSH session closed", m.window)
		}
	})

	form := widget.NewForm(
		widget.NewFormItem("Ubuntu Machine Host / IP", m.hostEntry),
		widget.NewFormItem("SSH Port", m.portEntry),
		widget.NewFormItem("SSH Username", m.userEntry),
		widget.NewFormItem("Private Key Path", m.keyEntry),
		widget.NewFormItem("Password (Alternative)", m.passEntry),
	)

	btnRow := container.NewHBox(connectBtn, disconnectBtn, m.connLabel)
	desc := widget.NewLabel("Connect directly to your Ubuntu host running NeonServices and attached StationPC PocketCloud NAS.")
	desc.Wrapping = fyne.TextWrapWord

	return container.NewVBox(desc, form, btnRow)
}

func (m *ManagerApp) connectSSH() {
	port, err := strconv.Atoi(m.portEntry.Text)
	if err != nil {
		port = 22
	}

	opts := sshutil.Options{
		Host:     strings.TrimSpace(m.hostEntry.Text),
		Port:     port,
		User:     strings.TrimSpace(m.userEntry.Text),
		KeyPath:  strings.TrimSpace(m.keyEntry.Text),
		Password: m.passEntry.Text,
		Timeout:  8 * time.Second,
	}

	if m.sshClient != nil {
		_ = m.sshClient.Close()
	}

	client := sshutil.NewClient(opts)
	m.connLabel.SetText("Status: Connecting...")

	go func() {
		err := client.Connect()
		if err != nil {
			m.window.Canvas().Refresh(m.connLabel)
			m.connLabel.SetText("Status: Connection Failed")
			dialog.ShowError(fmt.Errorf("SSH connection failed: %w", err), m.window)
			return
		}
		m.sshClient = client
		out, _ := client.Run("uname -a && uptime")
		m.connLabel.SetText("Status: Connected to " + opts.Host)
		dialog.ShowInformation("Connected", fmt.Sprintf("Successfully connected to Ubuntu host!\n\n%s", out), m.window)
	}()
}

func (m *ManagerApp) buildConfigTab() fyne.CanvasObject {
	m.serverPortEntry = widget.NewEntry()
	m.serverPortEntry.SetText("8080")

	m.jwtSecretEntry = widget.NewEntry()
	m.jwtSecretEntry.SetText("super-secret-jwt-key-replace-me")

	genSecretBtn := widget.NewButtonWithIcon("Generate Secure Key", theme.ContentAddIcon(), func() {
		b := make([]byte, 32)
		_, _ = rand.Read(b)
		m.jwtSecretEntry.SetText(hex.EncodeToString(b))
	})

	m.dbPathEntry = widget.NewEntry()
	m.dbPathEntry.SetText("/var/lib/neonservices/neonservices.db")

	m.nasMountEntry = widget.NewEntry()
	m.nasMountEntry.SetText("/mnt/pocketcloud/storage")

	m.quotaGBEntry = widget.NewEntry()
	m.quotaGBEntry.SetText("50")

	m.uploadMBEntry = widget.NewEntry()
	m.uploadMBEntry.SetText("2048")

	m.rawConfigEntry = widget.NewMultiLineEntry()
	m.rawConfigEntry.Wrapping = fyne.TextWrapWord
	m.rawConfigEntry.SetMinRowsVisible(8)

	fetchBtn := widget.NewButtonWithIcon("Pull Remote Config", theme.DownloadIcon(), func() {
		m.fetchRemoteConfig()
	})

	deployBtn := widget.NewButtonWithIcon("Push Config & Secrets via SSH", theme.UploadIcon(), func() {
		m.pushConfigSSH()
	})
	deployBtn.Importance = widget.HighImportance

	secretRow := container.NewBorder(nil, nil, nil, genSecretBtn, m.jwtSecretEntry)

	form := widget.NewForm(
		widget.NewFormItem("Server Port", m.serverPortEntry),
		widget.NewFormItem("JWT Secret Key", secretRow),
		widget.NewFormItem("SQLite DB Path", m.dbPathEntry),
		widget.NewFormItem("NAS Base Mount Path", m.nasMountEntry),
		widget.NewFormItem("Default User Quota (GB)", m.quotaGBEntry),
		widget.NewFormItem("Max Upload Size (MB)", m.uploadMBEntry),
	)

	btnRow := container.NewHBox(fetchBtn, deployBtn)

	return container.NewVBox(
		widget.NewLabel("Configure environment, secrets, and NAS storage mount parameters:"),
		form,
		btnRow,
		widget.NewLabel("YAML Preview / Manual Adjustments:"),
		container.NewHScroll(m.rawConfigEntry),
	)
}

func (m *ManagerApp) fetchRemoteConfig() {
	if m.sshClient == nil {
		dialog.ShowInformation("Not Connected", "Please connect to the Ubuntu host via the SSH tab first.", m.window)
		return
	}

	remotePath := "/opt/neonservices/config.yaml"
	go func() {
		data, err := m.sshClient.DownloadContent(remotePath)
		if err != nil {
			dialog.ShowError(fmt.Errorf("Failed to read %s: %w", remotePath, err), m.window)
			return
		}

		var cfg config.Config
		if err := yaml.Unmarshal(data, &cfg); err == nil {
			m.serverPortEntry.SetText(fmt.Sprintf("%d", cfg.Server.Port))
			m.jwtSecretEntry.SetText(cfg.Auth.JWTSecret)
			m.dbPathEntry.SetText(cfg.Database.Path)
			m.nasMountEntry.SetText(cfg.Storage.BaseMountPath)
			m.quotaGBEntry.SetText(fmt.Sprintf("%d", cfg.Storage.DefaultQuotaBytes/(1024*1024*1024)))
			m.uploadMBEntry.SetText(fmt.Sprintf("%d", cfg.Storage.MaxUploadSizeMB))
		}
		m.rawConfigEntry.SetText(string(data))
		dialog.ShowInformation("Success", "Remote configuration loaded successfully!", m.window)
	}()
}

func (m *ManagerApp) pushConfigSSH() {
	if m.sshClient == nil {
		dialog.ShowInformation("Not Connected", "Please connect to the Ubuntu host via the SSH tab first.", m.window)
		return
	}

	port, _ := strconv.Atoi(m.serverPortEntry.Text)
	if port <= 0 {
		port = 8080
	}
	quotaGB, _ := strconv.ParseInt(m.quotaGBEntry.Text, 10, 64)
	if quotaGB <= 0 {
		quotaGB = 50
	}
	uploadMB, _ := strconv.ParseInt(m.uploadMBEntry.Text, 10, 64)
	if uploadMB <= 0 {
		uploadMB = 2048
	}

	cfg := config.DefaultConfig()
	cfg.Server.Port = port
	cfg.Auth.JWTSecret = m.jwtSecretEntry.Text
	cfg.Database.Path = m.dbPathEntry.Text
	cfg.Storage.BaseMountPath = m.nasMountEntry.Text
	cfg.Storage.DefaultQuotaBytes = quotaGB * 1024 * 1024 * 1024
	cfg.Storage.MaxUploadSizeMB = uploadMB

	yamlBytes, err := yaml.Marshal(cfg)
	if err != nil {
		dialog.ShowError(err, m.window)
		return
	}

	m.rawConfigEntry.SetText(string(yamlBytes))

	remotePath := "/opt/neonservices/config.yaml"
	go func() {
		err := m.sshClient.UploadContent(remotePath, yamlBytes, 0600)
		if err != nil {
			dialog.ShowError(fmt.Errorf("Failed to upload config to %s: %w", remotePath, err), m.window)
			return
		}
		dialog.ShowInformation("Configuration Deployed",
			fmt.Sprintf("Successfully pushed configuration to %s (permissions 0600).\nRestart the service to apply changes.", remotePath),
			m.window,
		)
	}()
}

func (m *ManagerApp) buildServiceTab() fyne.CanvasObject {
	m.serviceStatusLabel = widget.NewLabel("Service Status: Unknown")
	m.logsEntry = widget.NewMultiLineEntry()
	m.logsEntry.SetMinRowsVisible(14)
	m.logsEntry.Wrapping = fyne.TextWrapWord

	statusBtn := widget.NewButtonWithIcon("Check Status", theme.InfoIcon(), func() {
		m.refreshStatus()
	})

	restartBtn := widget.NewButtonWithIcon("Restart Service", theme.ViewRefreshIcon(), func() {
		if m.sshClient == nil {
			dialog.ShowInformation("Not Connected", "Please connect to SSH first.", m.window)
			return
		}
		go func() {
			out, err := m.sshClient.RestartService("neonservices")
			if err != nil {
				dialog.ShowError(fmt.Errorf("Restart error: %v\nOutput: %s", err, out), m.window)
			} else {
				dialog.ShowInformation("Service Restarted", "neonservices restarted successfully.", m.window)
				m.refreshStatus()
			}
		}()
	})

	logsBtn := widget.NewButtonWithIcon("Fetch Live Logs", theme.DocumentIcon(), func() {
		if m.sshClient == nil {
			dialog.ShowInformation("Not Connected", "Please connect to SSH first.", m.window)
			return
		}
		go func() {
			logs, err := m.sshClient.ServiceLogs("neonservices", 100)
			if err != nil {
				dialog.ShowError(err, m.window)
				return
			}
			m.logsEntry.SetText(logs)
		}()
	})

	btnRow := container.NewHBox(statusBtn, restartBtn, logsBtn, m.serviceStatusLabel)

	return container.NewBorder(
		container.NewVBox(widget.NewLabel("Ubuntu Systemd Service Management:"), btnRow),
		nil, nil, nil,
		container.NewVScroll(m.logsEntry),
	)
}

func (m *ManagerApp) refreshStatus() {
	if m.sshClient == nil {
		dialog.ShowInformation("Not Connected", "Please connect to SSH first.", m.window)
		return
	}
	go func() {
		status, isActive, _ := m.sshClient.ServiceStatus("neonservices")
		if isActive {
			m.serviceStatusLabel.SetText("Service Status: ACTIVE (running)")
		} else {
			m.serviceStatusLabel.SetText("Service Status: " + status)
		}
	}()
}

func (m *ManagerApp) buildNASTab() fyne.CanvasObject {
	m.nasStatusText = widget.NewLabel("NAS Status: Not queried. Connect via SSH to inspect.")
	m.nasStatusText.Wrapping = fyne.TextWrapWord

	inspectBtn := widget.NewButtonWithIcon("Inspect NAS & Filesystem", theme.SearchIcon(), func() {
		if m.sshClient == nil {
			dialog.ShowInformation("Not Connected", "Please connect to SSH first.", m.window)
			return
		}
		go func() {
			mountPath := m.nasMountEntry.Text
			cmd := fmt.Sprintf("df -h %s && ls -la %s 2>&1", mountPath, mountPath)
			out, err := m.sshClient.Run(cmd)
			if err != nil {
				m.nasStatusText.SetText(fmt.Sprintf("Failed or unmounted:\n%s\n%v", out, err))
			} else {
				m.nasStatusText.SetText(fmt.Sprintf("StationPC PocketCloud NAS Storage Status:\n\n%s", out))
			}
		}()
	})

	return container.NewVBox(
		widget.NewLabel("StationPC PocketCloud NAS Storage Mount Overview:"),
		inspectBtn,
		m.nasStatusText,
	)
}
