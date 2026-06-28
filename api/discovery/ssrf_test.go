package discovery

import (
	"errors"
	"net"
	neturl "net/url"
	"testing"
)

func TestValidateResolvedFetchURLRejectsUnsafeAddresses(t *testing.T) {
	tests := []string{
		"127.0.0.1",
		"10.0.0.1",
		"172.16.0.1",
		"192.168.1.1",
		"169.254.1.1",
		"169.254.169.254",
		"::1",
		"fc00::1",
	}
	for _, rawIP := range tests {
		t.Run(rawIP, func(t *testing.T) {
			parsedURL, err := neturl.Parse("http://example.com")
			if err != nil {
				t.Fatal(err)
			}
			err = ValidateResolvedFetchURL(parsedURL, []net.IP{net.ParseIP(rawIP)})
			if !errors.Is(err, ErrUnsafeIPAddress) {
				t.Fatalf("err = %v, want ErrUnsafeIPAddress", err)
			}
		})
	}
}

func TestValidateResolvedFetchURLAllowsPublicWebAddress(t *testing.T) {
	parsedURL, err := neturl.Parse("https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateResolvedFetchURL(parsedURL, []net.IP{net.ParseIP("93.184.216.34")}); err != nil {
		t.Fatalf("ValidateResolvedFetchURL returned error: %v", err)
	}
}

func TestParseFetchURLRejectsUnsafeSchemeAndPort(t *testing.T) {
	if _, err := parseFetchURL("file:///etc/passwd"); !errors.Is(err, ErrUnsafeURLScheme) {
		t.Fatalf("scheme err = %v, want ErrUnsafeURLScheme", err)
	}
	if _, err := parseFetchURL("http://example.com:22"); !errors.Is(err, ErrUnsafeURLPort) {
		t.Fatalf("port err = %v, want ErrUnsafeURLPort", err)
	}
	if _, err := parseFetchURL("http:///missing-host"); !errors.Is(err, ErrUnsafeURLHost) {
		t.Fatalf("host err = %v, want ErrUnsafeURLHost", err)
	}
}
