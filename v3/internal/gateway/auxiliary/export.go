package auxiliary

// AdapterFor exposes the same native operation adapters used by the gateway
// to administrator channel probes. Unsupported providers remain explicit nil.
func AdapterFor(provider string) Adapter { return defaultAdapters()[provider] }
