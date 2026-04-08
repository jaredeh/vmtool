package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jaredeh/vmtool/pkg/vmtool"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	statusStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	promptStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
)

type viewMode int

const (
	modeList viewMode = iota
	modeCreate
	modePlaybook
)

// fieldKind determines how a form field is edited.
type fieldKind int

const (
	fieldText   fieldKind = iota // free-text input
	fieldSelect                  // pick from a list with ←/→
)

type formField struct {
	label   string
	def     string
	kind    fieldKind
	options []string // populated at runtime for fieldSelect
}

// createForm field indices
const (
	fName = iota
	fImage
	fVCPUs
	fMemory
	fNetType
	fNetSource
	fSSHUser
	fSSHPass
	fPlaybook
	numCreateFields
)

type model struct {
	manager *vmtool.Manager
	vms     []vmtool.VMInfo
	cursor  int
	err     error
	status  string

	mode       viewMode
	formField  int
	formFields []formField
	formValues []string

	// playbook picker (for running VMs)
	playbooks    []string
	playbookIdx  int
}

type refreshMsg struct {
	vms []vmtool.VMInfo
	err error
}

type inventoryMsg struct {
	vms    []vmtool.VMInfo
	err    error
	ip     string
	status string
}

func refresh(m *vmtool.Manager) tea.Cmd {
	return func() tea.Msg {
		vms, err := m.List()
		return refreshMsg{vms: vms, err: err}
	}
}

func initialModel(mgr *vmtool.Manager) model {
	return model{manager: mgr}
}

func (m model) Init() tea.Cmd {
	return refresh(m.manager)
}

// --- Update dispatch ---

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case refreshMsg:
		m.vms = msg.vms
		m.err = msg.err
		if m.cursor >= len(m.vms) && len(m.vms) > 0 {
			m.cursor = len(m.vms) - 1
		}
		return m, nil

	case inventoryMsg:
		m.vms = msg.vms
		m.err = msg.err
		if m.cursor >= len(m.vms) && len(m.vms) > 0 {
			m.cursor = len(m.vms) - 1
		}
		m.status = msg.status
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case modeCreate:
			return m.updateCreate(msg)
		case modePlaybook:
			return m.updatePlaybook(msg)
		default:
			return m.updateList(msg)
		}
	}
	return m, nil
}

// --- List mode ---

func (m model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.vms)-1 {
			m.cursor++
		}
	case "r":
		m.status = "refreshing..."
		return m, refresh(m.manager)
	case "c":
		return m.enterCreateMode()
	case "p":
		return m.enterPlaybookMode()
	case "s", "S":
		if len(m.vms) > 0 {
			vm := m.vms[m.cursor]
			switch vm.State {
			case vmtool.StateShutoff:
				err := m.manager.Start(vm.Name)
				if err != nil {
					m.status = fmt.Sprintf("error: %v", err)
				} else {
					m.status = fmt.Sprintf("started %s", vm.Name)
				}
				return m, refresh(m.manager)
			case vmtool.StateRunning:
				err := m.manager.Stop(vm.Name)
				if err != nil {
					m.status = fmt.Sprintf("error: %v", err)
				} else {
					m.status = fmt.Sprintf("shutdown requested for %s", vm.Name)
				}
				return m, refresh(m.manager)
			default:
				m.status = fmt.Sprintf("%s is %s, cannot start/stop", vm.Name, vm.State)
			}
		}
	case "D":
		if len(m.vms) > 0 {
			vm := m.vms[m.cursor]
			err := m.manager.Delete(vm.Name)
			if err != nil {
				m.status = fmt.Sprintf("error: %v", err)
			} else {
				m.status = fmt.Sprintf("deleted %s", vm.Name)
			}
			return m, refresh(m.manager)
		}
	}
	return m, nil
}

// --- Create mode ---

func (m model) enterCreateMode() (tea.Model, tea.Cmd) {
	images, _ := m.manager.ListImages()
	playbooks, _ := vmtool.ListPlaybooks("ansible")

	// Build playbook options: "(none)" plus all found playbooks
	pbOptions := []string{"(none)"}
	pbOptions = append(pbOptions, playbooks...)

	fields := []formField{
		{label: "Name", kind: fieldText},
		{label: "Image", kind: fieldSelect, options: images},
		{label: "VCPUs", def: "2", kind: fieldText},
		{label: "Memory (MiB)", def: "2048", kind: fieldText},
		{label: "Net type", kind: fieldSelect, options: []string{"nat", "bridge", "direct"}},
		{label: "Net source", def: "default", kind: fieldText},
		{label: "SSH user", def: "packer", kind: fieldText},
		{label: "SSH pass", def: "packer", kind: fieldText},
		{label: "Playbook", kind: fieldSelect, options: pbOptions},
	}

	values := make([]string, len(fields))
	for i, f := range fields {
		if f.kind == fieldSelect && len(f.options) > 0 {
			values[i] = f.options[0]
		} else {
			values[i] = f.def
		}
	}

	m.mode = modeCreate
	m.formField = 0
	m.formFields = fields
	m.formValues = values
	m.status = ""
	return m, nil
}

func (m model) updateCreate(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	f := m.formFields[m.formField]

	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = modeList
		m.status = ""
		return m, nil
	case "tab", "down":
		if m.formField < len(m.formFields)-1 {
			m.formField++
		}
	case "shift+tab", "up":
		if m.formField > 0 {
			m.formField--
		}
	case "left":
		if f.kind == fieldSelect {
			m.cycleSelect(-1)
		}
	case "right":
		if f.kind == fieldSelect {
			m.cycleSelect(1)
		}
	case "backspace":
		if f.kind == fieldText {
			v := m.formValues[m.formField]
			if len(v) > 0 {
				m.formValues[m.formField] = v[:len(v)-1]
			}
		}
	case "enter":
		if m.formField < len(m.formFields)-1 {
			m.formField++
			return m, nil
		}
		return m.submitCreate()
	default:
		if f.kind == fieldText && len(key) == 1 {
			m.formValues[m.formField] += key
		}
	}
	return m, nil
}

func (m *model) cycleSelect(dir int) {
	f := m.formFields[m.formField]
	if len(f.options) == 0 {
		return
	}
	cur := 0
	for i, o := range f.options {
		if o == m.formValues[m.formField] {
			cur = i
			break
		}
	}
	cur += dir
	if cur < 0 {
		cur = len(f.options) - 1
	} else if cur >= len(f.options) {
		cur = 0
	}
	m.formValues[m.formField] = f.options[cur]
}

func (m model) submitCreate() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.formValues[fName])
	image := m.formValues[fImage]
	if name == "" || image == "" {
		m.status = "name and image are required"
		return m, nil
	}

	diskPath, err := m.manager.ImagePath(image)
	if err != nil {
		m.status = fmt.Sprintf("error: %v", err)
		return m, nil
	}

	vcpus, err := strconv.ParseUint(strings.TrimSpace(m.formValues[fVCPUs]), 10, 32)
	if err != nil || vcpus == 0 {
		m.status = "invalid vcpus"
		return m, nil
	}

	mem, err := strconv.ParseUint(strings.TrimSpace(m.formValues[fMemory]), 10, 32)
	if err != nil || mem == 0 {
		m.status = "invalid memory"
		return m, nil
	}

	netType := m.formValues[fNetType]
	netSource := strings.TrimSpace(m.formValues[fNetSource])
	sshUser := strings.TrimSpace(m.formValues[fSSHUser])
	sshPass := strings.TrimSpace(m.formValues[fSSHPass])
	playbookName := m.formValues[fPlaybook]

	cfg := vmtool.VMConfig{
		Name:      name,
		VCPUs:     uint(vcpus),
		MemoryMiB: uint(mem),
		DiskPath:  diskPath,
		Network: vmtool.NetworkConfig{
			Type:   vmtool.NetworkType(netType),
			Source: netSource,
		},
		SSHUser: sshUser,
		SSHPass: sshPass,
	}

	if err := m.manager.Create(cfg); err != nil {
		m.status = fmt.Sprintf("error: %v", err)
		m.mode = modeList
		return m, refresh(m.manager)
	}

	m.status = fmt.Sprintf("created %s, waiting for IP...", name)
	m.mode = modeList
	mgr := m.manager
	return m, func() tea.Msg {
		ip, err := mgr.WaitForIP(name, 30*1e9)
		if err != nil {
			return inventoryMsg{status: fmt.Sprintf("VM created but no IP: %v", err)}
		}
		_ = vmtool.WriteInventory("ansible/inventory.yml", ip, sshUser, sshPass)

		statusText := fmt.Sprintf("inventory written (IP: %s)", ip)

		if playbookName != "(none)" {
			playbookPath := "ansible/" + playbookName
			if err := vmtool.RunPlaybook("ansible/inventory.yml", playbookPath); err != nil {
				statusText += fmt.Sprintf(" | playbook error: %v", err)
			} else {
				statusText += fmt.Sprintf(" | playbook %s done", playbookName)
				if err := mgr.Reboot(name); err != nil {
					statusText += fmt.Sprintf(" | reboot error: %v", err)
				} else {
					statusText += " | rebooting"
				}
			}
		}

		vms, listErr := mgr.List()
		return inventoryMsg{vms: vms, err: listErr, ip: ip, status: statusText}
	}
}

// --- Playbook mode (run on existing VM) ---

func (m model) enterPlaybookMode() (tea.Model, tea.Cmd) {
	if len(m.vms) == 0 {
		m.status = "no VMs"
		return m, nil
	}
	vm := m.vms[m.cursor]
	if vm.State != vmtool.StateRunning {
		m.status = fmt.Sprintf("%s is not running", vm.Name)
		return m, nil
	}
	if vm.IP == "" {
		m.status = fmt.Sprintf("%s has no IP", vm.Name)
		return m, nil
	}

	playbooks, err := vmtool.ListPlaybooks("ansible")
	if err != nil || len(playbooks) == 0 {
		m.status = "no playbooks found in ansible/"
		return m, nil
	}

	m.mode = modePlaybook
	m.playbooks = playbooks
	m.playbookIdx = 0
	m.status = ""
	return m, nil
}

func (m model) updatePlaybook(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = modeList
		m.status = ""
		return m, nil
	case "up", "k":
		if m.playbookIdx > 0 {
			m.playbookIdx--
		}
	case "down", "j":
		if m.playbookIdx < len(m.playbooks)-1 {
			m.playbookIdx++
		}
	case "enter":
		return m.runPlaybook()
	}
	return m, nil
}

func (m model) runPlaybook() (tea.Model, tea.Cmd) {
	vm := m.vms[m.cursor]
	pb := m.playbooks[m.playbookIdx]

	m.status = fmt.Sprintf("running %s on %s...", pb, vm.Name)
	m.mode = modeList

	mgr := m.manager
	vmName := vm.Name
	vmIP := vm.IP
	return m, func() tea.Msg {
		// Ensure inventory has correct IP
		_, _ = vmtool.EnsureInventory("ansible/inventory.yml", vmIP, "packer", "packer")

		playbookPath := "ansible/" + pb
		statusText := ""
		if err := vmtool.RunPlaybook("ansible/inventory.yml", playbookPath); err != nil {
			statusText = fmt.Sprintf("playbook error: %v", err)
		} else {
			statusText = fmt.Sprintf("playbook %s done on %s", pb, vmName)
		}

		vms, listErr := mgr.List()
		return inventoryMsg{vms: vms, err: listErr, ip: vmIP, status: statusText}
	}
}

// --- Views ---

func (m model) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("vmtool"))
	b.WriteString("\n\n")

	switch m.mode {
	case modeCreate:
		return b.String() + m.viewCreate()
	case modePlaybook:
		return b.String() + m.viewPlaybook()
	}

	if m.err != nil {
		b.WriteString(fmt.Sprintf("Error: %v\n", m.err))
		return b.String()
	}

	if len(m.vms) == 0 {
		b.WriteString("No VMs found.\n")
	} else {
		header := fmt.Sprintf("  %-20s %-10s %-6s %-10s %s", "NAME", "STATE", "VCPUS", "MEMORY", "IP")
		b.WriteString(statusStyle.Render(header))
		b.WriteString("\n")

		for i, vm := range m.vms {
			ip := vm.IP
			if ip == "" {
				ip = "-"
			}
			line := fmt.Sprintf("  %-20s %-10s %-6d %-10s %s",
				vm.Name, vm.State, vm.VCPUs, fmt.Sprintf("%d MiB", vm.MemoryMiB), ip)

			if i == m.cursor {
				b.WriteString(selectedStyle.Render("> " + line[2:]))
			} else {
				b.WriteString(line)
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	if m.status != "" {
		b.WriteString(statusStyle.Render(m.status))
		b.WriteString("\n")
	}
	b.WriteString(helpStyle.Render("c=create  s=start/stop  p=playbook  D=delete  r=refresh  q=quit"))
	b.WriteString("\n")

	return b.String()
}

func (m model) viewCreate() string {
	var b strings.Builder
	b.WriteString(promptStyle.Render("Create VM"))
	b.WriteString("\n\n")

	for i, f := range m.formFields {
		cursor := "  "
		if i == m.formField {
			cursor = "> "
		}
		val := m.formValues[i]
		if f.kind == fieldSelect {
			if i == m.formField {
				val = fmt.Sprintf("◀ %s ▶", val)
			}
		} else {
			if i == m.formField {
				val += "█"
			}
		}
		b.WriteString(fmt.Sprintf("%s%-20s %s\n", cursor, f.label+":", val))
	}

	b.WriteString("\n")
	if m.status != "" {
		b.WriteString(statusStyle.Render(m.status))
		b.WriteString("\n")
	}
	b.WriteString(helpStyle.Render("tab/↓=next  ↑=prev  ←/→=select  enter=submit  esc=cancel"))
	b.WriteString("\n")
	return b.String()
}

func (m model) viewPlaybook() string {
	var b strings.Builder
	vm := m.vms[m.cursor]
	b.WriteString(promptStyle.Render(fmt.Sprintf("Run playbook on %s (%s)", vm.Name, vm.IP)))
	b.WriteString("\n\n")

	for i, pb := range m.playbooks {
		if i == m.playbookIdx {
			b.WriteString(selectedStyle.Render("> " + pb))
		} else {
			b.WriteString("  " + pb)
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("↑/↓=select  enter=run  esc=cancel"))
	b.WriteString("\n")
	return b.String()
}

// Run launches the interactive TUI.
func Run() error {
	mgr, err := vmtool.NewManager()
	if err != nil {
		return err
	}
	defer mgr.Close()

	p := tea.NewProgram(initialModel(mgr))
	_, err = p.Run()
	return err
}
