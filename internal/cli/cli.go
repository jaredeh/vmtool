package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

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
		destroyCmd(),
		deleteCmd(),
		listCmd(),
		infoCmd(),
		autostartCmd(),
		interactiveCmd(),
		networksCmd(),
		playbookCmd(),
		imagesCmd(),
		poolCmd(),
		migrateDiskCmd(),
	)

	return root.Execute()
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

func createCmd() *cobra.Command {
	var (
		vcpus      uint
		memoryMiB  uint
		diskSizeGB uint
		pool       string
		netType    string
		netSource  string
		sshUser    string
		sshPass    string
		inventory  string
		playbook   string
		noclone    bool
	)

	cmd := &cobra.Command{
		Use:   "create <name> <image>",
		Short: "Define and start a new VM",
		Long:  `Define and start a new VM from a disk image.

By default the image is cloned so the original is unchanged.
Use --noclone to boot directly from the image; any changes (e.g. package
upgrades) will be written back to the source image.`,
		Args:  cobra.ExactArgs(2),
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			name := args[0]
			diskPath, err := m.ImagePath(args[1])
			if err != nil {
				return fmt.Errorf("resolving image: %w", err)
			}
			cfg := vmtool.DefaultConfig(name, diskPath)
			if vcpus > 0 {
				cfg.VCPUs = vcpus
			}
			if memoryMiB > 0 {
				cfg.MemoryMiB = memoryMiB
			}
			if diskSizeGB > 0 {
				cfg.DiskSizeGB = diskSizeGB
			}
			if netType != "" {
				cfg.Network.Type = vmtool.NetworkType(netType)
			}
			if pool != "" {
				cfg.Pool = pool
			}
			if netSource != "" {
				cfg.Network.Source = netSource
			}
			if sshUser != "" {
				cfg.SSHUser = sshUser
			}
			if sshPass != "" {
				cfg.SSHPass = sshPass
			}
			cfg.Noclone = noclone
			if err := m.Create(cfg); err != nil {
				return err
			}
			fmt.Printf("VM %q created and started\n", name)
			ip, err := m.WaitForIP(name, 120*1e9) // 120s
			if err == nil {
				fmt.Printf("IP: %s\n", ip)
				if inventory != "" {
					if err := vmtool.WriteInventory(inventory, ip, cfg.SSHUser, cfg.SSHPass); err != nil {
						return fmt.Errorf("writing inventory: %w", err)
					}
					fmt.Printf("Inventory written to %s\n", inventory)
				}
			}
			if cfg.DiskSizeGB > 0 && ip != "" {
				if err := vmtool.GrowDisk(inventory); err != nil {
					return fmt.Errorf("growing disk: %w", err)
				}
				fmt.Println("Disk partition and filesystem expanded")
			}
			if playbook != "" {
				if ip == "" {
					return fmt.Errorf("cannot run playbook: VM has no IP")
				}
				output, err := vmtool.RunPlaybook(inventory, playbook)
				fmt.Print(output)
				if err != nil {
					return fmt.Errorf("running playbook: %w", err)
				}
				fmt.Printf("Playbook %s completed\n", playbook)
				if err := m.Reboot(name); err != nil {
					return fmt.Errorf("rebooting VM: %w", err)
				}
				fmt.Printf("VM %q rebooted\n", name)
			}
			return nil
		}),
	}

	cmd.Flags().UintVar(&vcpus, "vcpus", 0, "number of virtual CPUs (default 2)")
	cmd.Flags().UintVar(&memoryMiB, "memory", 0, "memory in MiB (default 2048)")
	cmd.Flags().UintVar(&diskSizeGB, "disk-size", 0, "disk size in GB (default: same as base image)")
	cmd.Flags().StringVar(&netType, "net-type", "", "network type: nat, bridge, direct")
	cmd.Flags().StringVar(&netSource, "net-source", "", "network source (network name, bridge, or host interface)")
	cmd.Flags().StringVar(&sshUser, "ssh-user", "", "SSH username (default packer)")
	cmd.Flags().StringVar(&sshPass, "ssh-pass", "", "SSH password (default packer)")
	cmd.Flags().StringVar(&inventory, "inventory", "ansible/inventory.yml", "path to write Ansible inventory file")
	cmd.Flags().StringVar(&playbook, "playbook", "", "path to Ansible playbook to run after creation")
	cmd.Flags().StringVar(&pool, "pool", "", "storage pool for the cloned disk (default: default)")
	cmd.Flags().BoolVar(&noclone, "noclone", false, "boot directly from the image without cloning (changes persist to source)")

	return cmd
}

func startCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start <name>",
		Short: "Start a stopped VM",
		Args:  cobra.ExactArgs(1),
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			if err := m.Start(args[0]); err != nil {
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
			if err := m.Resume(args[0]); err != nil {
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
			if err := m.Stop(args[0]); err != nil {
				return err
			}
			fmt.Printf("VM %q shutdown requested\n", args[0])
			return nil
		}),
	}
}

func destroyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "destroy <name>",
		Short: "Force power-off a VM",
		Args:  cobra.ExactArgs(1),
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			if err := m.Destroy(args[0]); err != nil {
				return err
			}
			fmt.Printf("VM %q destroyed\n", args[0])
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
		Args:  cobra.ExactArgs(1),
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			if err := m.Delete(args[0], noclone); err != nil {
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
		Use:   "list",
		Short: "List all VMs",
		Aliases: []string{"ls"},
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			vms, err := m.List()
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
				fmt.Fprintf(w, "%s\t%s\t%d\t%d MiB\t%s\t%s\n", vm.Name, vm.State, vm.VCPUs, vm.MemoryMiB, autostart, ip)
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
			info, err := m.Info(args[0])
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
			fmt.Printf("Memory:    %d MiB\n", info.MemoryMiB)
			fmt.Printf("Autostart: %s\n", autostart)
			fmt.Printf("IP:        %s\n", ip)
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
			if err := m.SetAutostart(name, enable); err != nil {
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
			nets, err := m.ListNetworks()
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

func playbookCmd() *cobra.Command {
	var (
		sshUser   string
		sshPass   string
		inventory string
	)

	cmd := &cobra.Command{
		Use:   "playbook <vm-name> <playbook-path>",
		Short: "Run an Ansible playbook against a running VM",
		Args:  cobra.ExactArgs(2),
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			name := args[0]
			playbookPath := args[1]

			info, err := m.Info(name)
			if err != nil {
				return err
			}
			if info.IP == "" {
				return fmt.Errorf("VM %q has no IP address", name)
			}

			updated, err := vmtool.EnsureInventory(inventory, info.IP, sshUser, sshPass)
			if err != nil {
				return fmt.Errorf("ensuring inventory: %w", err)
			}
			if updated {
				fmt.Printf("Inventory %s updated with IP %s\n", inventory, info.IP)
			}

			output, err := vmtool.RunPlaybook(inventory, playbookPath)
			fmt.Print(output)
			if err != nil {
				return fmt.Errorf("running playbook: %w", err)
			}
			fmt.Printf("Playbook %s completed\n", playbookPath)
			return nil
		}),
	}

	cmd.Flags().StringVar(&sshUser, "ssh-user", "packer", "SSH username")
	cmd.Flags().StringVar(&sshPass, "ssh-pass", "packer", "SSH password")
	cmd.Flags().StringVar(&inventory, "inventory", "ansible/inventory.yml", "path to Ansible inventory file")

	return cmd
}

func imagesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "images",
		Short: "List available disk images",
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			images, err := m.ListImages()
			if err != nil {
				return err
			}
			for _, img := range images {
				fmt.Println(img)
			}
			return nil
		}),
	}
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
			pools, err := m.ListPools()
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
			if err := m.CreatePool(args[0], args[1]); err != nil {
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
			if err := m.MigrateDisk(vmName, pool); err != nil {
				return err
			}
			fmt.Printf("VM %q disk migrated to pool %q\n", vmName, pool)
			return nil
		}),
	}
}
