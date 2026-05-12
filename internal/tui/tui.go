package tui

import (
	"fmt"
	"path/filepath"
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
	borderStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	logRunning    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	logDone       = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	logError      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	logSelected   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
)

type viewMode int

const (
	modeList viewMode = iota
	modeCreate
	modePlaybook
	modeOutput
	modeConfirm
	modeBridge
)

type pane int

const (
	paneVMs pane = iota
	paneLog
)

// fieldKind determines how a form field is edited.
type fieldKind int

const (
	fieldText   fieldKind = iota
	fieldSelect
)

type formField struct {
	label    string
	def      string
	kind     fieldKind
	options  []string
	hidden   bool
	password bool
}

const (
	fName = iota
	fImage
	fDiskSize
	fVCPUs
	fMemory
	fNetType
	fNetSource
	fMacvtapMode
	fSSHUser
	fSSHPass
	fPlaybook
	numCreateFields
)

const (
	bfName = iota
	bfNIC
	bfIPMode
	bfIP
	bfGateway
	bfDNS
	bfSudoPass
	numBridgeFields
)

// --- Command log ---

type logStatus int

const (
	logStatusRunning logStatus = iota
	logStatusDone
	logStatusError
)

type logEntry struct {
	title  string
	status logStatus
	output string
}

// --- Model ---

type model struct {
	manager *vmtool.Manager
	vms     []vmtool.VMInfo
	cursor  int
	err     error

	mode       viewMode
	focus      pane
	formField  int
	formFields []formField
	formValues []string

	playbooks   []string
	playbookIdx int

	// command log
	log       []logEntry
	logCursor int

	// confirm dialog
	confirmMsg    string
	confirmAction func() (tea.Model, tea.Cmd)

	// output modal
	outputTitle  string
	outputLines  []string
	outputScroll int

	termHeight int
	termWidth  int

	playbookDir   string
	inventoryPath string

	// cached for dynamic net source options
	bridges  []string
	networks []string
	nics     []string

	// bridge creation form
	bridgeField  int
	bridgeFields []formField
	bridgeValues []string
}

type refreshMsg struct {
	vms []vmtool.VMInfo
	err error
}

type cmdDoneMsg struct {
	logIdx int
	title  string
	status logStatus
	output string
	vms    []vmtool.VMInfo
	err    error
}

// createCtx carries state across chained create stages.
type createCtx struct {
	cfg          vmtool.VMConfig
	playbookName string
	sshUser      string
	sshPass      string
	invPath      string
	pbDir        string
	ip           string
}

type bridgeCreatedMsg struct {
	logIdx  int
	name    string
	bridges []string
}

type createStageMsg struct {
	doneIdx int       // log entry to mark done
	title   string    // updated title for doneIdx
	status  logStatus // status for doneIdx
	output  string    // output for doneIdx (e.g. playbook)
	vms     []vmtool.VMInfo
	err     error
	// next stage
	nextTitle string    // title for the next log entry (empty = no next stage)
	nextFn    func(nextIdx int) tea.Msg // function for next stage
}

func refresh(m *vmtool.Manager) tea.Cmd {
	return func() tea.Msg {
		vms, err := m.List()
		return refreshMsg{vms: vms, err: err}
	}
}

func (m *model) addLog(title string) int {
	m.log = append(m.log, logEntry{title: title, status: logStatusRunning})
	return len(m.log) - 1
}

func initialModel(mgr *vmtool.Manager, playbookDir, inventoryPath string) model {
	return model{
		manager:       mgr,
		playbookDir:   playbookDir,
		inventoryPath: inventoryPath,
	}
}

func (m model) Init() tea.Cmd {
	return refresh(m.manager)
}

// --- Update dispatch ---

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termHeight = msg.Height
		m.termWidth = msg.Width
		return m, nil

	case refreshMsg:
		m.vms = msg.vms
		m.err = msg.err
		if m.cursor >= len(m.vms) && len(m.vms) > 0 {
			m.cursor = len(m.vms) - 1
		}
		return m, nil

	case cmdDoneMsg:
		if msg.logIdx >= 0 && msg.logIdx < len(m.log) {
			m.log[msg.logIdx].status = msg.status
			m.log[msg.logIdx].title = msg.title
			m.log[msg.logIdx].output = msg.output
		}
		if msg.vms != nil {
			m.vms = msg.vms
			m.err = msg.err
			if m.cursor >= len(m.vms) && len(m.vms) > 0 {
				m.cursor = len(m.vms) - 1
			}
		}
		return m, nil

	case bridgeCreatedMsg:
		if msg.logIdx >= 0 && msg.logIdx < len(m.log) {
			m.log[msg.logIdx].status = logStatusDone
			m.log[msg.logIdx].title = fmt.Sprintf("bridge %s created", msg.name)
		}
		m.bridges = msg.bridges
		// Update net source options if we're in bridge mode
		if m.formValues != nil && len(m.formValues) > fNetType && m.formValues[fNetType] == "bridge" {
			opts := append([]string{}, m.bridges...)
			opts = append(opts, "New Bridge...")
			m.formFields[fNetSource] = formField{label: "Bridge", kind: fieldSelect, options: opts}
			m.formValues[fNetSource] = msg.name
		}
		return m, nil

	case createStageMsg:
		// Complete the current stage's log entry
		if msg.doneIdx >= 0 && msg.doneIdx < len(m.log) {
			m.log[msg.doneIdx].status = msg.status
			m.log[msg.doneIdx].title = msg.title
			m.log[msg.doneIdx].output = msg.output
		}
		if msg.vms != nil {
			m.vms = msg.vms
			m.err = msg.err
			if m.cursor >= len(m.vms) && len(m.vms) > 0 {
				m.cursor = len(m.vms) - 1
			}
		}
		// Chain to next stage if there is one
		if msg.nextFn != nil {
			nextIdx := m.addLog(msg.nextTitle)
			fn := msg.nextFn
			return m, func() tea.Msg { return fn(nextIdx) }
		}
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case modeConfirm:
			return m.updateConfirm(msg)
		case modeOutput:
			return m.updateOutput(msg)
		case modeCreate:
			return m.updateCreate(msg)
		case modeBridge:
			return m.updateBridge(msg)
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
	case "tab":
		if m.focus == paneVMs {
			m.focus = paneLog
		} else {
			m.focus = paneVMs
		}
	case "up", "k":
		if m.focus == paneVMs {
			if m.cursor > 0 {
				m.cursor--
			}
		} else {
			if m.logCursor > 0 {
				m.logCursor--
			}
		}
	case "down", "j":
		if m.focus == paneVMs {
			if m.cursor < len(m.vms)-1 {
				m.cursor++
			}
		} else {
			if m.logCursor < len(m.log)-1 {
				m.logCursor++
			}
		}
	case "enter":
		if m.focus == paneLog && len(m.log) > 0 {
			entry := m.log[m.logCursor]
			if entry.output != "" {
				m.outputTitle = entry.title
				m.outputLines = strings.Split(strings.TrimRight(entry.output, "\n"), "\n")
				m.outputScroll = 0
				m.mode = modeOutput
			}
		}
	case "r":
		return m, refresh(m.manager)
	case "c":
		if m.focus == paneVMs {
			return m.enterCreateMode()
		}
	case "p":
		if m.focus == paneVMs {
			return m.enterPlaybookMode()
		}
	case "s", "S":
		if m.focus == paneVMs && len(m.vms) > 0 {
			vm := m.vms[m.cursor]
			switch vm.State {
			case vmtool.StateShutoff:
				err := m.manager.Start(vm.Name)
				if err != nil {
					idx := m.addLog(fmt.Sprintf("start %s: error: %v", vm.Name, err))
					m.log[idx].status = logStatusError
				} else {
					idx := m.addLog(fmt.Sprintf("started %s", vm.Name))
					m.log[idx].status = logStatusDone
				}
				return m, refresh(m.manager)
			case vmtool.StateRunning:
				vmName := vm.Name
				m.confirmMsg = fmt.Sprintf("Shut down %q?", vmName)
				m.confirmAction = func() (tea.Model, tea.Cmd) {
					err := m.manager.Stop(vmName)
					if err != nil {
						idx := m.addLog(fmt.Sprintf("stop %s: error: %v", vmName, err))
						m.log[idx].status = logStatusError
					} else {
						idx := m.addLog(fmt.Sprintf("shutdown requested for %s", vmName))
						m.log[idx].status = logStatusDone
					}
					m.mode = modeList
					return m, refresh(m.manager)
				}
				m.mode = modeConfirm
			}
		}
	case "a":
		if m.focus == paneVMs && len(m.vms) > 0 {
			vm := m.vms[m.cursor]
			newState := !vm.Autostart
			err := m.manager.SetAutostart(vm.Name, newState)
			if err != nil {
				idx := m.addLog(fmt.Sprintf("autostart %s: error: %v", vm.Name, err))
				m.log[idx].status = logStatusError
			} else {
				label := "enabled"
				if !newState {
					label = "disabled"
				}
				idx := m.addLog(fmt.Sprintf("autostart %s for %s", label, vm.Name))
				m.log[idx].status = logStatusDone
			}
			return m, refresh(m.manager)
		}
	case "D":
		if m.focus == paneVMs && len(m.vms) > 0 {
			vm := m.vms[m.cursor]
			vmName := vm.Name
			m.confirmMsg = fmt.Sprintf("Delete %q? This will destroy the VM and remove its disk.", vmName)
			m.confirmAction = func() (tea.Model, tea.Cmd) {
				err := m.manager.Delete(vmName)
				if err != nil {
					idx := m.addLog(fmt.Sprintf("delete %s: error: %v", vmName, err))
					m.log[idx].status = logStatusError
				} else {
					idx := m.addLog(fmt.Sprintf("deleted %s", vmName))
					m.log[idx].status = logStatusDone
				}
				m.mode = modeList
				return m, refresh(m.manager)
			}
			m.mode = modeConfirm
		}
	}
	return m, nil
}

// --- Confirm dialog ---

func (m model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		return m.confirmAction()
	case "n", "N", "esc":
		m.mode = modeList
	}
	return m, nil
}

// --- Create mode ---

func (m model) enterCreateMode() (tea.Model, tea.Cmd) {
	images, _ := m.manager.ListImages()
	playbooks, _ := vmtool.ListPlaybooks(m.playbookDir)

	pbOptions := []string{"(none)"}
	pbOptions = append(pbOptions, playbooks...)

	networks, _ := m.manager.ListNetworks()
	if len(networks) == 0 {
		networks = []string{"default"}
	}
	bridges, _ := m.manager.ListBridges()
	nics, _ := vmtool.ListPhysicalNICs()

	fields := []formField{
		{label: "Name", kind: fieldText},
		{label: "Image", kind: fieldSelect, options: images},
		{label: "Disk size (GB)", def: "default", kind: fieldText},
		{label: "VCPUs", def: "2", kind: fieldText},
		{label: "Memory (MiB)", def: "2048", kind: fieldText},
		{label: "Net type", kind: fieldSelect, options: []string{"nat", "bridge", "direct"}},
		{label: "Net source", kind: fieldSelect, options: networks},
		{label: "Macvtap mode", kind: fieldSelect, options: []string{"bridge", "vepa", "private", "passthrough"}, hidden: true},
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

	// Store lists for dynamic switching
	m.bridges = bridges
	m.networks = networks
	m.nics = nics

	m.mode = modeCreate
	m.formField = 0
	m.formFields = fields
	m.formValues = values
	return m, nil
}

func (m *model) nextVisibleField(from, dir int) int {
	i := from + dir
	for i >= 0 && i < len(m.formFields) {
		if !m.formFields[i].hidden {
			return i
		}
		i += dir
	}
	return from
}

func (m model) updateCreate(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	f := m.formFields[m.formField]

	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = modeList
		return m, nil
	case "tab", "down":
		m.formField = m.nextVisibleField(m.formField, 1)
	case "shift+tab", "up":
		m.formField = m.nextVisibleField(m.formField, -1)
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
		// "New Bridge..." opens bridge creation form
		if m.formField == fNetSource && m.formValues[fNetSource] == "New Bridge..." {
			return m.enterBridgeMode()
		}
		next := m.nextVisibleField(m.formField, 1)
		if next != m.formField {
			m.formField = next
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

	// When net type changes, update net source options
	if m.formField == fNetType {
		m.updateNetSourceOptions()
	}
}

// updateNetSourceOptions switches the Net source field options based on the
// currently selected Net type.
func (m *model) updateNetSourceOptions() {
	netType := m.formValues[fNetType]
	switch vmtool.NetworkType(netType) {
	case vmtool.NetworkBridge:
		opts := append([]string{}, m.bridges...)
		opts = append(opts, "New Bridge...")
		m.formFields[fNetSource] = formField{label: "Bridge", kind: fieldSelect, options: opts}
		m.formValues[fNetSource] = opts[0]
		m.formFields[fMacvtapMode].hidden = true
	case vmtool.NetworkDirect:
		if len(m.nics) > 0 {
			m.formFields[fNetSource] = formField{label: "Host NIC", kind: fieldSelect, options: m.nics}
			m.formValues[fNetSource] = m.nics[0]
		} else {
			m.formFields[fNetSource] = formField{label: "Host NIC", kind: fieldText}
			m.formValues[fNetSource] = ""
		}
		m.formFields[fMacvtapMode].hidden = false
	default: // nat
		m.formFields[fNetSource] = formField{label: "Net source", kind: fieldSelect, options: m.networks}
		if len(m.networks) > 0 {
			m.formValues[fNetSource] = m.networks[0]
		}
		m.formFields[fMacvtapMode].hidden = true
	}
}

func (m model) submitCreate() (tea.Model, tea.Cmd) {
	// Redirect to bridge creation if "New Bridge..." is still selected
	if m.formValues[fNetSource] == "New Bridge..." {
		return m.enterBridgeMode()
	}

	name := strings.TrimSpace(m.formValues[fName])
	image := m.formValues[fImage]
	if name == "" || image == "" {
		return m, nil
	}

	diskPath, err := m.manager.ImagePath(image)
	if err != nil {
		idx := m.addLog(fmt.Sprintf("create %s: %v", name, err))
		m.log[idx].status = logStatusError
		m.mode = modeList
		return m, nil
	}

	var diskSizeGB uint
	diskSizeStr := strings.TrimSpace(m.formValues[fDiskSize])
	if diskSizeStr != "" && diskSizeStr != "default" {
		ds, err := strconv.ParseUint(diskSizeStr, 10, 32)
		if err != nil || ds == 0 {
			return m, nil
		}
		diskSizeGB = uint(ds)
	}

	vcpus, _ := strconv.ParseUint(strings.TrimSpace(m.formValues[fVCPUs]), 10, 32)
	if vcpus == 0 {
		vcpus = 2
	}
	mem, _ := strconv.ParseUint(strings.TrimSpace(m.formValues[fMemory]), 10, 32)
	if mem == 0 {
		mem = 2048
	}

	netType := m.formValues[fNetType]
	netSource := strings.TrimSpace(m.formValues[fNetSource])
	sshUser := strings.TrimSpace(m.formValues[fSSHUser])
	sshPass := strings.TrimSpace(m.formValues[fSSHPass])
	playbookName := m.formValues[fPlaybook]

	cfg := vmtool.VMConfig{
		Name:       name,
		VCPUs:      uint(vcpus),
		MemoryMiB:  uint(mem),
		DiskPath:   diskPath,
		DiskSizeGB: diskSizeGB,
		Network: vmtool.NetworkConfig{
			Type:        vmtool.NetworkType(netType),
			Source:      netSource,
			MacvtapMode: vmtool.MacvtapMode(m.formValues[fMacvtapMode]),
		},
		SSHUser: sshUser,
		SSHPass: sshPass,
	}

	ctx := createCtx{
		cfg:          cfg,
		playbookName: playbookName,
		sshUser:      sshUser,
		sshPass:      sshPass,
		invPath:      m.inventoryPath,
		pbDir:        m.playbookDir,
	}

	logIdx := m.addLog(fmt.Sprintf("cloning %s from %s...", name, image))
	m.mode = modeList
	mgr := m.manager
	return m, func() tea.Msg {
		return stageClone(mgr, ctx, logIdx)
	}
}

func stageClone(mgr *vmtool.Manager, ctx createCtx, logIdx int) tea.Msg {
	name := ctx.cfg.Name
	image := filepath.Base(ctx.cfg.DiskPath)
	clonedPath, err := mgr.CloneImage(image, name)
	if err != nil {
		return createStageMsg{
			doneIdx: logIdx,
			title:   fmt.Sprintf("%s: clone error: %v", name, err),
			status:  logStatusError,
		}
	}
	ctx.cfg.DiskPath = clonedPath

	msg := createStageMsg{
		doneIdx: logIdx,
		title:   fmt.Sprintf("%s: cloned %s → %s.qcow2", name, image, name),
		status:  logStatusDone,
	}

	if ctx.cfg.DiskSizeGB > 0 {
		msg.nextTitle = fmt.Sprintf("resizing disk to %dGB...", ctx.cfg.DiskSizeGB)
		msg.nextFn = func(nextIdx int) tea.Msg { return stageResize(mgr, ctx, nextIdx) }
	} else {
		msg.nextTitle = fmt.Sprintf("starting %s...", name)
		msg.nextFn = func(nextIdx int) tea.Msg { return stageStart(mgr, ctx, nextIdx) }
	}
	return msg
}

func stageResize(mgr *vmtool.Manager, ctx createCtx, logIdx int) tea.Msg {
	name := ctx.cfg.Name
	sizeBytes := uint64(ctx.cfg.DiskSizeGB) * 1024 * 1024 * 1024
	err := mgr.ResizeVolume(name+".qcow2", sizeBytes)

	if err != nil {
		return createStageMsg{
			doneIdx: logIdx,
			title:   fmt.Sprintf("%s: resize error: %v", name, err),
			status:  logStatusError,
		}
	}
	return createStageMsg{
		doneIdx:   logIdx,
		title:     fmt.Sprintf("%s: disk resized to %dGB", name, ctx.cfg.DiskSizeGB),
		status:    logStatusDone,
		nextTitle: fmt.Sprintf("starting %s...", name),
		nextFn:    func(nextIdx int) tea.Msg { return stageStart(mgr, ctx, nextIdx) },
	}
}

func stageStart(mgr *vmtool.Manager, ctx createCtx, logIdx int) tea.Msg {
	name := ctx.cfg.Name
	if err := mgr.Define(ctx.cfg); err != nil {
		return createStageMsg{
			doneIdx: logIdx,
			title:   fmt.Sprintf("%s: define error: %v", name, err),
			status:  logStatusError,
		}
	}
	if err := mgr.Start(name); err != nil {
		return createStageMsg{
			doneIdx: logIdx,
			title:   fmt.Sprintf("%s: start error: %v", name, err),
			status:  logStatusError,
		}
	}
	vms, _ := mgr.List()
	return createStageMsg{
		doneIdx:   logIdx,
		title:     fmt.Sprintf("%s: started", name),
		status:    logStatusDone,
		vms:       vms,
		nextTitle: fmt.Sprintf("waiting for IP on %s...", name),
		nextFn:    func(nextIdx int) tea.Msg { return stageWaitIP(mgr, ctx, nextIdx) },
	}
}

func stageWaitIP(mgr *vmtool.Manager, ctx createCtx, logIdx int) tea.Msg {
	name := ctx.cfg.Name
	ip, err := mgr.WaitForIP(name, 120*1e9)
	if err != nil {
		vms, _ := mgr.List()
		return createStageMsg{
			doneIdx: logIdx,
			title:   fmt.Sprintf("%s: no IP: %v", name, err),
			status:  logStatusError,
			vms:     vms,
		}
	}
	ctx.ip = ip
	_ = vmtool.WriteInventory(ctx.invPath, ip, ctx.sshUser, ctx.sshPass)

	vms, _ := mgr.List()
	msg := createStageMsg{
		doneIdx: logIdx,
		title:   fmt.Sprintf("%s: IP %s", name, ip),
		status:  logStatusDone,
		vms:     vms,
	}

	if ctx.cfg.DiskSizeGB > 0 {
		msg.nextTitle = fmt.Sprintf("expanding partition on %s...", name)
		msg.nextFn = func(nextIdx int) tea.Msg { return stageGrowPartition(mgr, ctx, nextIdx) }
	} else if ctx.playbookName != "(none)" {
		msg.nextTitle = fmt.Sprintf("running %s on %s...", ctx.playbookName, name)
		msg.nextFn = func(nextIdx int) tea.Msg { return stagePlaybook(mgr, ctx, nextIdx) }
	}
	return msg
}

func stageGrowPartition(mgr *vmtool.Manager, ctx createCtx, logIdx int) tea.Msg {
	name := ctx.cfg.Name
	err := vmtool.GrowDisk(ctx.invPath)

	msg := createStageMsg{doneIdx: logIdx}
	if err != nil {
		msg.title = fmt.Sprintf("%s: partition expand error: %v", name, err)
		msg.status = logStatusError
	} else {
		msg.title = fmt.Sprintf("%s: partition expanded", name)
		msg.status = logStatusDone
	}

	if ctx.playbookName != "(none)" {
		msg.nextTitle = fmt.Sprintf("running %s on %s...", ctx.playbookName, name)
		msg.nextFn = func(nextIdx int) tea.Msg { return stagePlaybook(mgr, ctx, nextIdx) }
	}
	return msg
}

func stagePlaybook(mgr *vmtool.Manager, ctx createCtx, logIdx int) tea.Msg {
	name := ctx.cfg.Name
	playbookPath := filepath.Join(ctx.pbDir, ctx.playbookName)
	out, err := vmtool.RunPlaybook(ctx.invPath, playbookPath)

	if err != nil {
		return createStageMsg{
			doneIdx: logIdx,
			title:   fmt.Sprintf("%s: %s error: %v", name, ctx.playbookName, err),
			status:  logStatusError,
			output:  out,
		}
	}

	return createStageMsg{
		doneIdx:   logIdx,
		title:     fmt.Sprintf("%s: %s done", name, ctx.playbookName),
		status:    logStatusDone,
		output:    out,
		nextTitle: fmt.Sprintf("rebooting %s...", name),
		nextFn:    func(nextIdx int) tea.Msg { return stageReboot(mgr, ctx, nextIdx) },
	}
}

func stageReboot(mgr *vmtool.Manager, ctx createCtx, logIdx int) tea.Msg {
	name := ctx.cfg.Name
	err := mgr.Reboot(name)
	vms, _ := mgr.List()

	if err != nil {
		return createStageMsg{
			doneIdx: logIdx,
			title:   fmt.Sprintf("%s: reboot error: %v", name, err),
			status:  logStatusError,
			vms:     vms,
		}
	}
	return createStageMsg{
		doneIdx: logIdx,
		title:   fmt.Sprintf("%s: rebooted", name),
		status:  logStatusDone,
		vms:     vms,
	}
}

// --- Bridge creation mode ---

func (m model) enterBridgeMode() (tea.Model, tea.Cmd) {
	nics := m.nics
	if len(nics) == 0 {
		return m, nil
	}

	nicInfo := vmtool.GetNICInfo(nics[0])

	fields := []formField{
		{label: "Bridge name", def: "br0", kind: fieldText},
		{label: "Host NIC", kind: fieldSelect, options: nics},
		{label: "IP mode", kind: fieldSelect, options: []string{"dhcp", "static"}},
		{label: "IP/CIDR", kind: fieldText},
		{label: "Gateway", kind: fieldText},
		{label: "DNS", kind: fieldText},
		{label: "Sudo password", kind: fieldText, password: true},
	}

	values := make([]string, numBridgeFields)
	values[bfName] = "br0"
	values[bfNIC] = nics[0]
	if nicInfo.IP != "" {
		values[bfIPMode] = "static"
		values[bfIP] = nicInfo.IP
		values[bfGateway] = nicInfo.Gateway
		values[bfDNS] = nicInfo.DNS
	} else {
		values[bfIPMode] = "dhcp"
	}

	m.mode = modeBridge
	m.bridgeField = 0
	m.bridgeFields = fields
	m.bridgeValues = values
	m.updateBridgeFieldVisibility()
	return m, nil
}

func (m *model) updateBridgeFieldVisibility() {
	isStatic := m.bridgeValues[bfIPMode] == "static"
	m.bridgeFields[bfIP].hidden = !isStatic
	m.bridgeFields[bfGateway].hidden = !isStatic
	m.bridgeFields[bfDNS].hidden = !isStatic
}

func (m *model) nextVisibleBridgeField(from, dir int) int {
	i := from + dir
	for i >= 0 && i < len(m.bridgeFields) {
		if !m.bridgeFields[i].hidden {
			return i
		}
		i += dir
	}
	return from
}

func (m model) updateBridge(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	f := m.bridgeFields[m.bridgeField]

	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = modeCreate
		return m, nil
	case "tab", "down":
		m.bridgeField = m.nextVisibleBridgeField(m.bridgeField, 1)
	case "shift+tab", "up":
		m.bridgeField = m.nextVisibleBridgeField(m.bridgeField, -1)
	case "left":
		if f.kind == fieldSelect {
			m.cycleBridgeSelect(-1)
		}
	case "right":
		if f.kind == fieldSelect {
			m.cycleBridgeSelect(1)
		}
	case "backspace":
		if f.kind == fieldText {
			v := m.bridgeValues[m.bridgeField]
			if len(v) > 0 {
				m.bridgeValues[m.bridgeField] = v[:len(v)-1]
			}
		}
	case "enter":
		next := m.nextVisibleBridgeField(m.bridgeField, 1)
		if next != m.bridgeField {
			m.bridgeField = next
			return m, nil
		}
		return m.submitBridge()
	default:
		if f.kind == fieldText && len(key) == 1 {
			m.bridgeValues[m.bridgeField] += key
		}
	}
	return m, nil
}

func (m *model) cycleBridgeSelect(dir int) {
	f := m.bridgeFields[m.bridgeField]
	if len(f.options) == 0 {
		return
	}
	cur := 0
	for i, o := range f.options {
		if o == m.bridgeValues[m.bridgeField] {
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
	m.bridgeValues[m.bridgeField] = f.options[cur]

	// When NIC changes, update IP defaults
	if m.bridgeField == bfNIC {
		nicInfo := vmtool.GetNICInfo(m.bridgeValues[bfNIC])
		if nicInfo.IP != "" {
			m.bridgeValues[bfIPMode] = "static"
			m.bridgeValues[bfIP] = nicInfo.IP
			m.bridgeValues[bfGateway] = nicInfo.Gateway
			m.bridgeValues[bfDNS] = nicInfo.DNS
		} else {
			m.bridgeValues[bfIPMode] = "dhcp"
			m.bridgeValues[bfIP] = ""
			m.bridgeValues[bfGateway] = ""
			m.bridgeValues[bfDNS] = ""
		}
		m.updateBridgeFieldVisibility()
	}
	if m.bridgeField == bfIPMode {
		m.updateBridgeFieldVisibility()
	}
}

func (m model) submitBridge() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.bridgeValues[bfName])
	nic := m.bridgeValues[bfNIC]
	if name == "" || nic == "" {
		return m, nil
	}

	sudoPass := m.bridgeValues[bfSudoPass]
	if sudoPass == "" {
		return m, nil
	}

	cfg := vmtool.BridgeConfig{
		Name:     name,
		NIC:      nic,
		DHCP:     m.bridgeValues[bfIPMode] == "dhcp",
		IP:       strings.TrimSpace(m.bridgeValues[bfIP]),
		Gateway:  strings.TrimSpace(m.bridgeValues[bfGateway]),
		DNS:      strings.TrimSpace(m.bridgeValues[bfDNS]),
		SudoPass: sudoPass,
	}

	m.confirmMsg = fmt.Sprintf("Create bridge %q on %s? This will disrupt networking on %s.", name, nic, nic)
	m.confirmAction = func() (tea.Model, tea.Cmd) {
		brCfg := cfg
		logIdx := m.addLog(fmt.Sprintf("creating bridge %s on %s...", brCfg.Name, brCfg.NIC))
		m.mode = modeList
		mgr := m.manager
		return m, func() tea.Msg {
			err := vmtool.CreateBridge(brCfg)
			if err != nil {
				return cmdDoneMsg{
					logIdx: logIdx,
					title:  fmt.Sprintf("bridge %s: error: %v", brCfg.Name, err),
					status: logStatusError,
				}
			}
			// Refresh bridge list after creation
			newBridges, _ := mgr.ListBridges()
			return bridgeCreatedMsg{
				logIdx:  logIdx,
				name:    brCfg.Name,
				bridges: newBridges,
			}
		}
	}
	m.mode = modeConfirm
	return m, nil
}

// --- Playbook mode ---

func (m model) enterPlaybookMode() (tea.Model, tea.Cmd) {
	if len(m.vms) == 0 {
		return m, nil
	}
	vm := m.vms[m.cursor]
	if vm.State != vmtool.StateRunning || vm.IP == "" {
		return m, nil
	}

	playbooks, err := vmtool.ListPlaybooks(m.playbookDir)
	if err != nil || len(playbooks) == 0 {
		return m, nil
	}

	m.mode = modePlaybook
	m.playbooks = playbooks
	m.playbookIdx = 0
	return m, nil
}

func (m model) updatePlaybook(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = modeList
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

	logIdx := m.addLog(fmt.Sprintf("running %s on %s...", pb, vm.Name))
	m.mode = modeList

	mgr := m.manager
	vmName := vm.Name
	vmIP := vm.IP
	invPath := m.inventoryPath
	pbDir := m.playbookDir
	return m, func() tea.Msg {
		_, _ = vmtool.EnsureInventory(invPath, vmIP, "packer", "packer")

		playbookPath := filepath.Join(pbDir, pb)
		out, err := vmtool.RunPlaybook(invPath, playbookPath)
		status := logStatusDone
		title := fmt.Sprintf("%s on %s: done", pb, vmName)
		if err != nil {
			status = logStatusError
			title = fmt.Sprintf("%s on %s: error: %v", pb, vmName, err)
		}

		vms, listErr := mgr.List()
		return cmdDoneMsg{
			logIdx: logIdx,
			title:  title,
			status: status,
			output: out,
			vms:    vms,
			err:    listErr,
		}
	}
}

// --- Output mode ---

func (m model) updateOutput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	maxVisible := m.termHeight - 6
	if maxVisible < 5 {
		maxVisible = 5
	}
	switch msg.String() {
	case "q", "esc", "enter":
		m.mode = modeList
		return m, nil
	case "up", "k":
		if m.outputScroll > 0 {
			m.outputScroll--
		}
	case "down", "j":
		if m.outputScroll < len(m.outputLines)-maxVisible {
			m.outputScroll++
		}
	case "g":
		m.outputScroll = 0
	case "G":
		end := len(m.outputLines) - maxVisible
		if end < 0 {
			end = 0
		}
		m.outputScroll = end
	}
	return m, nil
}

// --- Views ---

func (m model) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("vmtool"))
	b.WriteString("\n\n")

	switch m.mode {
	case modeCreate:
		return b.String() + m.viewCreate()
	case modeBridge:
		return b.String() + m.viewBridge()
	case modePlaybook:
		return b.String() + m.viewPlaybook()
	case modeOutput:
		return b.String() + m.viewOutput()
	case modeConfirm:
		return b.String() + m.viewConfirm()
	}

	// --- VM list pane ---
	vmFocus := m.focus == paneVMs

	if m.err != nil {
		b.WriteString(fmt.Sprintf("Error: %v\n", m.err))
	} else if len(m.vms) == 0 {
		b.WriteString("  No VMs found.\n")
	} else {
		header := fmt.Sprintf("  %-20s %-10s %-6s %-10s %-11s %s", "NAME", "STATE", "VCPUS", "MEMORY", "AUTOSTART", "IP")
		b.WriteString(statusStyle.Render(header))
		b.WriteString("\n")

		for i, vm := range m.vms {
			ip := vm.IP
			if ip == "" {
				ip = "-"
			}
			autostart := "off"
			if vm.Autostart {
				autostart = "on"
			}
			line := fmt.Sprintf("  %-20s %-10s %-6d %-10s %-11s %s",
				vm.Name, vm.State, vm.VCPUs, fmt.Sprintf("%d MiB", vm.MemoryMiB), autostart, ip)

			if i == m.cursor && vmFocus {
				b.WriteString(selectedStyle.Render("> " + line[2:]))
			} else if i == m.cursor {
				b.WriteString(statusStyle.Render("> " + line[2:]))
			} else {
				b.WriteString(line)
			}
			b.WriteString("\n")
		}
	}

	// --- Separator ---
	sep := strings.Repeat("─", 70)
	if m.termWidth > 4 {
		sep = strings.Repeat("─", m.termWidth-2)
	}
	label := " Commands "
	if len(sep) > len(label)+4 {
		sep = sep[:2] + label + sep[2+len(label):]
	}
	b.WriteString(borderStyle.Render(sep))
	b.WriteString("\n")

	// --- Command log pane ---
	logFocus := m.focus == paneLog

	logHeight := 8
	if len(m.log) == 0 {
		b.WriteString(statusStyle.Render("  No commands yet."))
		b.WriteString("\n")
	} else {
		start := 0
		if len(m.log) > logHeight {
			start = len(m.log) - logHeight
			if logFocus && m.logCursor < start {
				start = m.logCursor
			}
		}
		end := start + logHeight
		if end > len(m.log) {
			end = len(m.log)
		}

		for i := start; i < end; i++ {
			entry := m.log[i]
			prefix := "  "
			var style lipgloss.Style

			switch entry.status {
			case logStatusRunning:
				style = logRunning
				prefix = "⟳ "
			case logStatusDone:
				style = logDone
				prefix = "✓ "
			case logStatusError:
				style = logError
				prefix = "✗ "
			}

			line := prefix + entry.title
			if entry.output != "" {
				line += " [↵]"
			}

			if i == m.logCursor && logFocus {
				b.WriteString(logSelected.Render("> " + line[2:]))
			} else {
				b.WriteString(style.Render(line))
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	if m.focus == paneVMs {
		b.WriteString(helpStyle.Render("c=create  s=start/stop  a=autostart  p=playbook  D=delete  r=refresh  tab=log  q=quit"))
	} else {
		b.WriteString(helpStyle.Render("enter=view output  tab=vms  q=quit"))
	}
	b.WriteString("\n")

	return b.String()
}

func (m model) viewCreate() string {
	var b strings.Builder
	b.WriteString(promptStyle.Render("Create VM"))
	b.WriteString("\n\n")

	for i, f := range m.formFields {
		if f.hidden {
			continue
		}
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
	b.WriteString(helpStyle.Render("tab/↓=next  ↑=prev  ←/→=select  enter=submit  esc=cancel"))
	b.WriteString("\n")
	return b.String()
}

func (m model) viewBridge() string {
	var b strings.Builder
	b.WriteString(promptStyle.Render("Create Bridge"))
	b.WriteString("\n\n")

	for i, f := range m.bridgeFields {
		if f.hidden {
			continue
		}
		cursor := "  "
		if i == m.bridgeField {
			cursor = "> "
		}
		val := m.bridgeValues[i]
		if f.password {
			val = strings.Repeat("•", len(val))
		}
		if f.kind == fieldSelect {
			if i == m.bridgeField {
				val = fmt.Sprintf("◀ %s ▶", val)
			}
		} else {
			if i == m.bridgeField {
				val += "█"
			}
		}
		b.WriteString(fmt.Sprintf("%s%-20s %s\n", cursor, f.label+":", val))
	}

	b.WriteString("\n")
	b.WriteString(logError.Render("  ⚠  This will disrupt networking on the selected NIC"))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("tab/↓=next  ↑=prev  ←/→=select  enter=submit  esc=back"))
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

func (m model) viewOutput() string {
	var b strings.Builder
	b.WriteString(promptStyle.Render(m.outputTitle))
	b.WriteString("\n\n")

	maxVisible := m.termHeight - 6
	if maxVisible < 5 {
		maxVisible = 5
	}

	end := m.outputScroll + maxVisible
	if end > len(m.outputLines) {
		end = len(m.outputLines)
	}

	for _, line := range m.outputLines[m.outputScroll:end] {
		b.WriteString(line)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	scrollInfo := fmt.Sprintf("[%d-%d of %d]", m.outputScroll+1, end, len(m.outputLines))
	b.WriteString(helpStyle.Render(fmt.Sprintf("↑/↓=scroll  g/G=top/bottom  esc=close  %s", scrollInfo)))
	b.WriteString("\n")
	return b.String()
}

func (m model) viewConfirm() string {
	var b strings.Builder
	b.WriteString(logError.Render("⚠  " + m.confirmMsg))
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("y/enter=confirm  n/esc=cancel"))
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

	playbookDir := filepath.Join("ansible", "playbooks")
	inventoryPath := filepath.Join("ansible", "inventory.yml")

	p := tea.NewProgram(initialModel(mgr, playbookDir, inventoryPath))
	_, err = p.Run()
	return err
}
