package credentials

import (
	"context"
	"net"

	utls "github.com/refraction-networking/utls"
)

func imageTLSConnection(ctx context.Context, conn net.Conn, host string, cfg TransportConfig, fp Fingerprint) (net.Conn, error) {
	hello := utls.HelloChrome_120
	if fp.TLSProfile == "firefox" {
		hello = utls.HelloFirefox_120
	}
	spec, err := utls.UTLSIdToSpec(hello)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	for i, extension := range spec.Extensions {
		switch extension.(type) {
		case *utls.ALPNExtension:
			spec.Extensions[i] = &utls.ALPNExtension{AlpnProtocols: []string{"http/1.1"}}
		case *utls.ApplicationSettingsExtension:
			spec.Extensions[i] = &utls.ApplicationSettingsExtension{SupportedProtocols: []string{"http/1.1"}}
		}
	}
	secured := utls.UClient(conn, &utls.Config{ServerName: host, RootCAs: cfg.RootCAs}, utls.HelloCustom)
	if err = secured.ApplyPreset(&spec); err == nil {
		handshake, cancel := context.WithTimeout(ctx, cfg.TLSHandshakeTimeout)
		err = secured.HandshakeContext(handshake)
		cancel()
	}
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return secured, nil
}
