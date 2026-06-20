package lansync

import (
	"fmt"
	"io"
	"net"
)

// printServeBanner tells the user how the other device should connect.
func printServeBanner(out io.Writer, port, code string) {
	if out == nil {
		return
	}
	fmt.Fprintln(out, "Waiting for a device on your local network to connect…")
	fmt.Fprintln(out)
	// The connect command intentionally does NOT include the code: passing it as
	// --code would leak the secret into the other shell's process list and history.
	// The code is shown separately and typed at the prompt that connect shows.
	ips := localPrivateIPv4()
	if len(ips) == 0 {
		fmt.Fprintf(out, "  On the other device run:  cct sync connect <this-ip>:%s --i-understand\n", port)
	} else {
		fmt.Fprintln(out, "  On the other device run one of:")
		for _, ip := range ips {
			fmt.Fprintf(out, "    cct sync connect %s:%s --i-understand\n", ip, port)
		}
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  When it asks, enter this pairing code:  %s\n", code)
	fmt.Fprintln(out, "  (it authenticates the peer; share it only with your own device.)")
	fmt.Fprintln(out)
}

// printPreview renders a dry-run summary.
func printPreview(out io.Writer, r Result) {
	if out == nil {
		return
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Dry run — nothing was written:")
	fmt.Fprintf(out, "  Sessions this device would send:    %d\n", r.PreviewSend)
	fmt.Fprintf(out, "  Sessions this device would receive: %d\n", r.PreviewRecv)
	fmt.Fprintln(out, "Re-run without --dry-run to apply (each session is merged append-only; conflicts are reported, never overwritten).")
}

// localPrivateIPv4 lists this machine's private IPv4 addresses, for the connect
// hint. Best-effort; returns nil if none are found.
func localPrivateIPv4() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []string
	for _, a := range addrs {
		var ip net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil || ip.To4() == nil {
			continue
		}
		if ip.IsPrivate() {
			out = append(out, ip.String())
		}
	}
	return out
}
