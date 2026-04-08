package vmtool

import "fmt"

// networkXML generates the libvirt XML fragment for the VM's network interface.
func networkXML(cfg NetworkConfig) string {
	switch cfg.Type {
	case NetworkBridge:
		return fmt.Sprintf(`    <interface type="bridge">
      <source bridge="%s"/>
      <model type="virtio"/>
    </interface>`, cfg.Source)
	case NetworkDirect:
		return fmt.Sprintf(`    <interface type="direct">
      <source dev="%s" mode="vepa"/>
      <model type="virtio"/>
    </interface>`, cfg.Source)
	default: // NetworkNAT
		src := cfg.Source
		if src == "" {
			src = "default"
		}
		return fmt.Sprintf(`    <interface type="network">
      <source network="%s"/>
      <model type="virtio"/>
    </interface>`, src)
	}
}
