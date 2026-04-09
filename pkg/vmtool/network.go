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
		mode := cfg.MacvtapMode
		if mode == "" {
			mode = MacvtapBridge
		}
		return fmt.Sprintf(`    <interface type="direct">
      <source dev="%s" mode="%s"/>
      <model type="virtio"/>
    </interface>`, cfg.Source, mode)
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
