package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/jaredeh/vmtool/internal/app"
	"github.com/jaredeh/vmtool/internal/server"
	"github.com/jaredeh/vmtool/internal/tui"
	"github.com/jaredeh/vmtool/pkg/vmtool"
	"github.com/spf13/cobra"
)

func Execute() error {
	root := &cobra.Command{
		Use:   "vmtool",
		Short: "Manage KVM/QEMU virtual machines",
	}

	root.AddCommand(
		createCmd(),
		startCmd(),
		stopCmd(),
		resumeCmd(),
		poweroffCmd(),
		deleteCmd(),
		listCmd(),
		infoCmd(),
		sshCmd(),
		cmdCmd(),
		rebootCmd(),
		autostartCmd(),
		interactiveCmd(),
		networksCmd(),
		playbookCmd(),
		imagesCmd(),
		poolCmd(),
		migrateDiskCmd(),
		resizeDiskCmd(),
		addDiskCmd(),
		serverCmd(),
	)

	err := root.Execute()
	if err == nil {
		return nil
	}
	var remote remoteExitError
	if errors.As(err, &remote) {
		os.Exit(remote.code)
	}
	return err
}

// remoteExitError is a remote command's exit status. Execute exits with
// that code and does not print an extra error line.
type remoteExitError struct {
	code int
}

func (e remoteExitError) Error() string {
	return fmt.Sprintf("exit status %d", e.code)
}

func withManager(fn func(m *vmtool.Manager, cmd *cobra.Command, args []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		m, err := vmtool.NewManager()
		if err != nil {
			return err
		}
		defer m.Close()
		return fn(m, cmd, args)
	}
}

func defaultService(inventory string) *app.Service {
	return &app.Service{
		PlaybookDir:   filepath.Join("ansible", "playbooks"),
		InventoryPath: inventory,
	}
}

func printAppErr(err error) error {
	var ae *app.Error
	if errors.As(err, &ae) && ae.Output != "" {
		fmt.Print(ae.Output)
	}
	return err
}

func createCmd() *cobra.Command {
	var (
		vcpus           uint
		memory          uint
		diskSizeGB      uint
		pool            string
		netType         string
		netSource       string
		macvtapMode     string
		sshUser         string
		sshPass         string
		inventory       string
		playbook        string
		noclone         bool
		extraDiskSizeGB uint
		extraDiskPool   string
		extraVars       []string
	)

	cmd := &cobra.Command{
		Use:   "create <name> <image>",
		Short: "Define and start a new VM",
		Long: `Define and start a new VM from a disk image.

By default the image is cloned so the original is unchanged.
Use --noclone to boot directly from the image; any changes (e.g. package
upgrades) will be written back to the source image.`,
		Args: cobra.ExactArgs(2),
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			extra, err := parseExtraVars(extraVars)
			if err != nil {
				return err
			}
			info, err := defaultService(inventory).CreateVM(cmd.Context(), m, app.CreateVMInput{
				Name:        args[0],
				Image:       args[1],
				VCPUs:       vcpus,
				Memory:      memory,
				DiskSizeGB:  diskSizeGB,
				Pool:        pool,
				NetType:     netType,
				NetSource:   netSource,
				MacvtapMode: macvtapMode,
				SSHUser:     sshUser,
				SSHPass:     sshPass,
				Playbook:        playbook,
				Noclone:         noclone,
				ExtraDiskSizeGB: extraDiskSizeGB,
				ExtraDiskPool:   extraDiskPool,
				ExtraVars:       extra,
				OnProgress: func(ev app.ProgressEvent) {
					if ev.Output != "" {
						fmt.Print(ev.Output)
					}
					if (ev.Status == "done" || ev.Status == "error") && ev.Detail != "" {
						fmt.Println(ev.Detail)
					}
				},
			})
			if info != nil {
				fmt.Printf("VM %q created and started\n", info.Name)
				if info.IP != "" {
					fmt.Printf("IP: %s\n", info.IP)
				}
			}
			return printAppErr(err)
		}),
	}

	cmd.Flags().UintVar(&vcpus, "vcpus", 0, "number of virtual CPUs (default 2)")
	cmd.Flags().UintVar(&memory, "memory", 0, "memory in GiB (default 2)")
	cmd.Flags().UintVar(&diskSizeGB, "disk-size", 0, "disk size in GB (default: same as base image)")
	cmd.Flags().StringVar(&netType, "net-type", "", "network type: nat, bridge, direct")
	cmd.Flags().StringVar(&netSource, "net-source", "", "network source (network name, bridge, or host interface)")
	cmd.Flags().StringVar(&macvtapMode, "macvtap-mode", "", "macvtap mode when --net-type=direct (default bridge). One of: bridge, vepa, private, passthrough")
	cmd.Flags().StringVar(&sshUser, "ssh-user", "", "SSH username (default packer)")
	cmd.Flags().StringVar(&sshPass, "ssh-pass", "", "SSH password (default packer)")
	cmd.Flags().StringVar(&inventory, "inventory", "ansible/inventory.yml", "path to write Ansible inventory file")
	cmd.Flags().StringVar(&playbook, "playbook", "", "playbook name under ansible/playbooks, or a path")
	cmd.Flags().StringVar(&pool, "pool", "", "storage pool for the cloned disk (default: default)")
	cmd.Flags().BoolVar(&noclone, "noclone", false, "boot directly from the image without cloning (changes persist to source)")
	cmd.Flags().UintVar(&extraDiskSizeGB, "extra-disk-size", 0, "create a second empty virtio disk (vdb) named <name>-2.qcow2")
	cmd.Flags().StringVar(&extraDiskPool, "extra-disk-pool", "", "storage pool for the extra disk (default: same as --pool)")
	cmd.Flags().StringArrayVar(&extraVars, "extra-var", nil, "ansible extra-var key=value for --playbook (repeatable). deviceid defaults to the VM name")

	return cmd
}

func startCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start <name>",
		Short: "Start a stopped VM",
		Args:  cobra.ExactArgs(1),
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			if _, err := defaultService("").StartVM(cmd.Context(), m, args[0]); err != nil {
				return err
			}
			fmt.Printf("VM %q started\n", args[0])
			return nil
		}),
	}
}

func resumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume <name>",
		Short: "Resume a paused VM",
		Args:  cobra.ExactArgs(1),
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			if _, err := defaultService("").ResumeVM(cmd.Context(), m, args[0]); err != nil {
				return err
			}
			fmt.Printf("VM %q resumed\n", args[0])
			return nil
		}),
	}
}

func stopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <name>",
		Short: "Gracefully shut down a VM",
		Args:  cobra.ExactArgs(1),
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			if _, err := defaultService("").StopVM(cmd.Context(), m, args[0]); err != nil {
				return err
			}
			fmt.Printf("VM %q shutdown requested\n", args[0])
			return nil
		}),
	}
}

func poweroffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "poweroff <name>",
		Short: "Force power-off a VM",
		Args:  cobra.ExactArgs(1),
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			if _, err := defaultService("").PoweroffVM(cmd.Context(), m, args[0]); err != nil {
				return err
			}
			fmt.Printf("VM %q powered off\n", args[0])
			return nil
		}),
	}
}

func deleteCmd() *cobra.Command {
	var noclone bool

	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Stop and undefine a VM",
		Long: `Stop and undefine a VM.

By default also deletes the cloned disk volume.
Use --noclone to skip volume deletion (use when the VM was created with --noclone).`,
		Args: cobra.ExactArgs(1),
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			if err := defaultService("").DeleteVM(cmd.Context(), m, args[0], noclone); err != nil {
				return err
			}
			fmt.Printf("VM %q deleted\n", args[0])
			return nil
		}),
	}

	cmd.Flags().BoolVar(&noclone, "noclone", false, "undefine only, do not delete the disk volume")
	return cmd
}

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List all VMs",
		Aliases: []string{"ls"},
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			vms, err := defaultService("").ListVMs(cmd.Context(), m)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tSTATE\tVCPUS\tMEMORY\tAUTOSTART\tIP")
			for _, vm := range vms {
				ip := vm.IP
				if ip == "" {
					ip = "-"
				}
				autostart := "off"
				if vm.Autostart {
					autostart = "on"
				}
				fmt.Fprintf(w, "%s\t%s\t%d\t%d GiB\t%s\t%s\n", vm.Name, vm.State, vm.VCPUs, vm.Memory, autostart, ip)
			}
			return w.Flush()
		}),
	}
}

func infoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info <name>",
		Short: "Show details of a VM",
		Args:  cobra.ExactArgs(1),
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			info, err := defaultService("").GetVM(cmd.Context(), m, args[0])
			if err != nil {
				return err
			}
			ip := info.IP
			if ip == "" {
				ip = "-"
			}
			autostart := "off"
			if info.Autostart {
				autostart = "on"
			}
			fmt.Printf("Name:      %s\n", info.Name)
			fmt.Printf("State:     %s\n", info.State)
			fmt.Printf("VCPUs:     %d\n", info.VCPUs)
			fmt.Printf("Memory:    %d GiB\n", info.Memory)
			fmt.Printf("Autostart: %s\n", autostart)
			fmt.Printf("IP:        %s\n", ip)
			return nil
		}),
	}
}

func sshCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ssh <name> [-- ssh-args...]",
		Short: "SSH into a VM using stored .machines credentials",
		Long: `SSH into a running VM. If .machines/<ip>/ (or .machines/<name>/)
exists, uses the username and key stored there. Otherwise falls back to packer.
Host key checking is disabled.`,
		Args: cobra.MinimumNArgs(1),
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			info, err := m.Info(args[0])
			if err != nil {
				return err
			}
			if info.IP == "" {
				return fmt.Errorf("VM %q has no IP address", args[0])
			}

			c, err := vmtool.SSHCmd(info.IP, info.Name, args[1:]...)
			if err != nil {
				return err
			}
			return c.Run()
		}),
	}
}

func cmdCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cmd <name> <command> [args...]",
		Short: "Run a command on a VM over SSH",
		Long: `Run a command on a running VM and pass through stdin, stdout, stderr,
and the remote exit code. Uses .machines credentials when present.

  vmtool cmd web uname -a
  vmtool cmd web -- sh -c 'echo hi | wc -c'`,
		Args:          cobra.MinimumNArgs(2),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			info, err := m.Info(args[0])
			if err != nil {
				return err
			}
			if info.IP == "" {
				return fmt.Errorf("VM %q has no IP address", args[0])
			}

			c, err := vmtool.RemoteCmd(info.IP, info.Name, args[1:])
			if err != nil {
				return err
			}
			err = c.Run()
			if err == nil {
				return nil
			}
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				return remoteExitError{code: ee.ExitCode()}
			}
			return err
		}),
	}
	cmd.Flags().SetInterspersed(false)
	return cmd
}

func rebootCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reboot <name>",
		Short: "Reboot a running VM",
		Long: `Ask the guest to reboot (ACPI). The domain stays defined.

Unlike stop (ACPI shutdown, then shutoff) or poweroff (force destroy),
the VM comes back up on its own if the guest handles the reboot.`,
		Args: cobra.ExactArgs(1),
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			if _, err := defaultService("").RebootVM(cmd.Context(), m, args[0]); err != nil {
				return err
			}
			fmt.Printf("VM %q rebooted\n", args[0])
			return nil
		}),
	}
}

func autostartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "autostart <name> <on|off>",
		Short: "Enable or disable autostart for a VM",
		Args:  cobra.ExactArgs(2),
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			name := args[0]
			var enable bool
			switch args[1] {
			case "on", "true", "yes", "1":
				enable = true
			case "off", "false", "no", "0":
				enable = false
			default:
				return fmt.Errorf("invalid value %q: use on or off", args[1])
			}
			if _, err := defaultService("").SetAutostart(cmd.Context(), m, name, enable); err != nil {
				return err
			}
			label := "disabled"
			if enable {
				label = "enabled"
			}
			fmt.Printf("Autostart %s for VM %q\n", label, name)
			return nil
		}),
	}
}

func interactiveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "i",
		Short:   "Launch interactive TUI",
		Aliases: []string{"interactive"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return tui.Run()
		},
	}
}

func networksCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "networks",
		Short: "List available libvirt networks",
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			nets, err := defaultService("").ListNetworks(cmd.Context(), m)
			if err != nil {
				return err
			}
			for _, n := range nets {
				fmt.Println(n)
			}
			return nil
		}),
	}
}

func parseExtraVars(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid --extra-var %q: want key=value", p)
		}
		out[k] = v
	}
	return out, nil
}

func playbookCmd() *cobra.Command {
	var (
		sshUser   string
		sshPass   string
		extraVars []string
	)

	cmd := &cobra.Command{
		Use:   "playbook <vm-name> <playbook-path>",
		Short: "Run an Ansible playbook against a running VM",
		Args:  cobra.ExactArgs(2),
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			extra, err := parseExtraVars(extraVars)
			if err != nil {
				return err
			}
			res, err := defaultService("").RunPlaybook(cmd.Context(), m, args[0], args[1], vmtool.Auth{User: sshUser, Password: sshPass}, extra)
			if res != nil && res.Output != "" {
				fmt.Print(res.Output)
			}
			if err != nil {
				return printAppErr(err)
			}
			fmt.Printf("Playbook %s completed\n", args[1])
			return nil
		}),
	}

	cmd.Flags().StringVar(&sshUser, "ssh-user", "packer", "SSH username if no .machines entry exists")
	cmd.Flags().StringVar(&sshPass, "ssh-pass", "packer", "SSH password if no .machines entry exists")
	cmd.Flags().StringArrayVar(&extraVars, "extra-var", nil, "ansible extra-var key=value (repeatable). deviceid defaults to the VM name")

	return cmd
}

func imagesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "images",
		Short: "Manage disk images",
	}
	cmd.AddCommand(imagesListCmd(), imagesDeleteCmd())
	return cmd
}

func imagesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available disk images grouped by pool",
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			byPool, err := defaultService("").ListImages(cmd.Context(), m)
			if err != nil {
				return err
			}
			pools := make([]string, 0, len(byPool))
			for p := range byPool {
				pools = append(pools, p)
			}
			sort.Strings(pools)
			for _, p := range pools {
				fmt.Printf("%s:\n", p)
				imgs := byPool[p]
				sort.Strings(imgs)
				for _, img := range imgs {
					fmt.Printf("  %s\n", img)
				}
			}
			return nil
		}),
	}
}

func imagesDeleteCmd() *cobra.Command {
	var pool string
	cmd := &cobra.Command{
		Use:   "delete <image>",
		Short: "Delete a disk image from a storage pool",
		Args:  cobra.ExactArgs(1),
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			if err := defaultService("").DeleteImage(cmd.Context(), m, args[0], pool); err != nil {
				return err
			}
			fmt.Printf("Image %q deleted from pool %q\n", args[0], pool)
			return nil
		}),
	}
	cmd.Flags().StringVar(&pool, "pool", "default", "storage pool containing the image")
	return cmd
}

func poolCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pool",
		Short: "Manage storage pools",
	}
	cmd.AddCommand(poolListCmd(), poolCreateCmd())
	return cmd
}

func poolListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List storage pools",
		Aliases: []string{"ls"},
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			pools, err := defaultService("").ListPools(cmd.Context(), m)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tPATH\tACTIVE")
			for _, p := range pools {
				active := "no"
				if p.Active {
					active = "yes"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n", p.Name, p.Path, active)
			}
			return w.Flush()
		}),
	}
}

func poolCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create <name> <path>",
		Short: "Create a new directory-type storage pool",
		Args:  cobra.ExactArgs(2),
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			if err := defaultService("").CreatePool(cmd.Context(), m, args[0], args[1]); err != nil {
				return err
			}
			fmt.Printf("Pool %q created at %s\n", args[0], args[1])
			return nil
		}),
	}
}

func migrateDiskCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate-disk <vm> <pool>",
		Short: "Move a VM's disk to a different storage pool",
		Long: `Stop the VM (if running), copy its disk volume to the target pool,
redefine the domain with the new path, then restart (if it was running).`,
		Args: cobra.ExactArgs(2),
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			vmName := args[0]
			pool := args[1]
			fmt.Printf("Migrating disk for VM %q to pool %q...\n", vmName, pool)
			if _, err := defaultService("").MigrateDisk(cmd.Context(), m, vmName, pool); err != nil {
				return err
			}
			fmt.Printf("VM %q disk migrated to pool %q\n", vmName, pool)
			return nil
		}),
	}
}

func resizeDiskCmd() *cobra.Command {
	var (
		sshUser string
		sshPass string
	)

	cmd := &cobra.Command{
		Use:   "resize-disk <vm> <size-gb>",
		Short: "Grow a VM's disk to a new size",
		Long: `Resize a VM's disk volume to the given size in GB (must be larger).

If the VM is running, also notifies the guest of the new size and expands
the root partition and filesystem via SSH.`,
		Args: cobra.ExactArgs(2),
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			vmName := args[0]
			sizeGB, err := strconv.ParseUint(args[1], 10, 32)
			if err != nil || sizeGB == 0 {
				return fmt.Errorf("invalid size %q: must be a positive integer (GB)", args[1])
			}

			fmt.Printf("Resizing disk for VM %q to %dGB...\n", vmName, sizeGB)
			res, err := defaultService("").ResizeDisk(cmd.Context(), m, vmName, uint(sizeGB), vmtool.Auth{User: sshUser, Password: sshPass})
			if err != nil {
				return err
			}
			fmt.Printf("VM %q disk resized to %dGB\n", vmName, sizeGB)
			if res.FilesystemGrown {
				fmt.Println("Disk partition and filesystem expanded")
				return nil
			}
			if res.GrowError != "" {
				fmt.Println(res.GrowError)
				return fmt.Errorf("growing filesystem: %s", res.GrowError)
			}
			fmt.Println("VM is not running with an IP; grow the filesystem after start")
			return nil
		}),
	}

	cmd.Flags().StringVar(&sshUser, "ssh-user", "packer", "SSH username if no .machines entry exists")
	cmd.Flags().StringVar(&sshPass, "ssh-pass", "packer", "SSH password if no .machines entry exists")
	return cmd
}

func addDiskCmd() *cobra.Command {
	var pool string

	cmd := &cobra.Command{
		Use:   "add-disk <vm> <size-gb>",
		Short: "Attach a new empty disk to a VM",
		Long: `Create an empty qcow2 volume and attach it as the next virtio disk (vdb, vdc, …).

Volume name is <vm>-2.qcow2 for the first extra disk, <vm>-3.qcow2 if called
again, and so on. If the VM is running the disk is hot-plugged and persisted.
If it is shut off the change is persisted and appears on the next start. The
guest sees a raw block device; it is not partitioned or formatted.

  vmtool add-disk web 50
  vmtool add-disk web 100 --pool fast`,
		Args: cobra.ExactArgs(2),
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			vmName := args[0]
			sizeGB, err := strconv.ParseUint(args[1], 10, 32)
			if err != nil || sizeGB == 0 {
				return fmt.Errorf("invalid size %q: must be a positive integer (GB)", args[1])
			}

			res, err := defaultService("").AddDisk(cmd.Context(), m, vmName, uint(sizeGB), pool)
			if err != nil {
				return err
			}
			fmt.Printf("Attached %s (%dGB) to VM %q as %s\n", res.Path, sizeGB, vmName, res.Target)
			return nil
		}),
	}

	cmd.Flags().StringVar(&pool, "pool", "", "storage pool for the new volume (default: default)")
	return cmd
}

func serverCmd() *cobra.Command {
	var listen string
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Start the REST API server",
		Long: `Start an HTTP server that exposes vmtool operations.
GET / is Swagger UI; GET /openapi.yaml is the spec. Default listen is 127.0.0.1:9473.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			s := &server.Server{Listen: listen}
			fmt.Printf("vmtool API listening on http://%s/\n", listen)
			return s.ListenAndServe()
		},
	}
	cmd.Flags().StringVar(&listen, "listen", "127.0.0.1:9473", "address to listen on")
	return cmd
}
