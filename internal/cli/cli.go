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
		destroyCmd(),
		deleteCmd(),
		listCmd(),
		infoCmd(),
		interactiveCmd(),
		networksCmd(),
		playbookCmd(),
		imagesCmd(),
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
		vcpus     uint
		memoryMiB uint
		netType   string
		netSource string
		sshUser   string
		sshPass   string
		inventory string
		playbook  string
	)

	cmd := &cobra.Command{
		Use:   "create <name> <image>",
		Short: "Define and start a new VM",
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
			if netType != "" {
				cfg.Network.Type = vmtool.NetworkType(netType)
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
			if err := m.Create(cfg); err != nil {
				return err
			}
			fmt.Printf("VM %q created and started\n", name)
			ip, err := m.WaitForIP(name, 30*1e9) // 30s
			if err == nil {
				fmt.Printf("IP: %s\n", ip)
				if inventory != "" {
					if err := vmtool.WriteInventory(inventory, ip, cfg.SSHUser, cfg.SSHPass); err != nil {
						return fmt.Errorf("writing inventory: %w", err)
					}
					fmt.Printf("Inventory written to %s\n", inventory)
				}
			}
			if playbook != "" {
				if ip == "" {
					return fmt.Errorf("cannot run playbook: VM has no IP")
				}
				if err := vmtool.RunPlaybook(inventory, playbook); err != nil {
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
	cmd.Flags().StringVar(&netType, "net-type", "", "network type: nat, bridge, direct")
	cmd.Flags().StringVar(&netSource, "net-source", "", "network source (network name, bridge, or host interface)")
	cmd.Flags().StringVar(&sshUser, "ssh-user", "", "SSH username (default packer)")
	cmd.Flags().StringVar(&sshPass, "ssh-pass", "", "SSH password (default packer)")
	cmd.Flags().StringVar(&inventory, "inventory", "ansible/inventory.yml", "path to write Ansible inventory file")
	cmd.Flags().StringVar(&playbook, "playbook", "", "path to Ansible playbook to run after creation")

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
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Stop and undefine a VM",
		Args:  cobra.ExactArgs(1),
		RunE: withManager(func(m *vmtool.Manager, cmd *cobra.Command, args []string) error {
			if err := m.Delete(args[0]); err != nil {
				return err
			}
			fmt.Printf("VM %q deleted\n", args[0])
			return nil
		}),
	}
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
			fmt.Fprintln(w, "NAME\tSTATE\tVCPUS\tMEMORY\tIP")
			for _, vm := range vms {
				ip := vm.IP
				if ip == "" {
					ip = "-"
				}
				fmt.Fprintf(w, "%s\t%s\t%d\t%d MiB\t%s\n", vm.Name, vm.State, vm.VCPUs, vm.MemoryMiB, ip)
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
			fmt.Printf("Name:   %s\n", info.Name)
			fmt.Printf("State:  %s\n", info.State)
			fmt.Printf("VCPUs:  %d\n", info.VCPUs)
			fmt.Printf("Memory: %d MiB\n", info.MemoryMiB)
			fmt.Printf("IP:     %s\n", ip)
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

			if err := vmtool.RunPlaybook(inventory, playbookPath); err != nil {
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
