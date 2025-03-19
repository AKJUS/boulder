// The identifier package defines types for RFC 8555 ACME identifiers.
//
// It exists as a separate package to prevent an import loop between the core
// and probs packages.
//
// Function naming conventions:
// - "New" creates a new instance from one or more simple base type inputs.
// - "From" and "To" extract information from, or compose, a more complex object.
package identifier

import (
	"crypto/x509"
	"net"
	"net/netip"
	"slices"
	"strings"

	corepb "github.com/letsencrypt/boulder/core/proto"
)

// IdentifierType is a named string type for registered ACME identifier types.
// See https://tools.ietf.org/html/rfc8555#section-9.7.7
type IdentifierType string

const (
	// TypeDNS is specified in RFC 8555 for TypeDNS type identifiers.
	TypeDNS = IdentifierType("dns")
	// TypeIP is specified in RFC 8738
	TypeIP = IdentifierType("ip")
)

// ACMEIdentifier is a struct encoding an identifier that can be validated. The
// protocol allows for different types of identifier to be supported (DNS
// names, IP addresses, etc.), but currently we only support RFC 8555 DNS type
// identifiers for domain names.
type ACMEIdentifier struct {
	// Type is the registered IdentifierType of the identifier.
	Type IdentifierType `json:"type"`
	// Value is the value of the identifier. For a DNS type identifier it is
	// a domain name.
	Value string `json:"value"`
}

// ACMEIdentifiers is a named type for a slice of ACME identifiers, so that
// methods can be applied to these slices.
type ACMEIdentifiers []ACMEIdentifier

func (i ACMEIdentifier) ToProto() *corepb.Identifier {
	return &corepb.Identifier{
		Type:  string(i.Type),
		Value: i.Value,
	}
}

func FromProto(ident *corepb.Identifier) ACMEIdentifier {
	return ACMEIdentifier{
		Type:  IdentifierType(ident.Type),
		Value: ident.Value,
	}
}

// FromProtoWithDefault can be removed after DnsNames are no longer used in
// RPCs. TODO(#8023)
func FromProtoWithDefault(ident *corepb.Identifier, name string) ACMEIdentifier {
	if ident == nil {
		return NewDNS(name)
	}
	return FromProto(ident)
}

// NewDNS is a convenience function for creating an ACMEIdentifier with Type
// "dns" for a given domain name.
func NewDNS(domain string) ACMEIdentifier {
	return ACMEIdentifier{
		Type:  TypeDNS,
		Value: domain,
	}
}

// NewDNSSlice is a convenience function for creating a slice of ACMEIdentifier
// with Type "dns" for a given slice of domain names.
func NewDNSSlice(input []string) ACMEIdentifiers {
	var out []ACMEIdentifier
	for _, in := range input {
		out = append(out, NewDNS(in))
	}
	return out
}

// NewIP is a convenience function for creating an ACMEIdentifier with Type "ip"
// for a given IP address.
func NewIP(ip netip.Addr) ACMEIdentifier {
	return ACMEIdentifier{
		Type: TypeIP,
		// RFC 8738, Sec. 3: The identifier value MUST contain the textual form
		// of the address as defined in RFC 1123, Sec. 2.1 for IPv4 and in RFC
		// 5952, Sec. 4 for IPv6.
		Value: ip.String(),
	}
}

// fromX509 extracts the Subject Alternative Names from a certificate or CSR's fields, and
// returns a slice of ACMEIdentifiers.
func fromX509(commonName string, dnsNames []string, ipAddresses []net.IP) []ACMEIdentifier {
	var sans []ACMEIdentifier
	for _, name := range dnsNames {
		sans = append(sans, NewDNS(name))
	}
	if commonName != "" {
		// Boulder won't generate certificates with a CN that's not also present
		// in the SANs, but such a certificate is possible. If appended, this is
		// deduplicated later with Normalize(). We assume the CN is a DNSName,
		// because CNs are untyped strings without metadata, and we will never
		// configure a Boulder profile to issue a certificate that contains both
		// an IP address identifier and a CN.
		sans = append(sans, NewDNS(commonName))
	}

	for _, ip := range ipAddresses {
		sans = append(sans, ACMEIdentifier{
			Type:  TypeIP,
			Value: ip.String(),
		})
	}

	return Normalize(sans)
}

// FromCert extracts the Subject Common Name and Subject Alternative Names from
// a certificate, and returns a slice of ACMEIdentifiers.
func FromCert(cert *x509.Certificate) []ACMEIdentifier {
	return fromX509(cert.Subject.CommonName, cert.DNSNames, cert.IPAddresses)
}

// FromCSR extracts the Subject Common Name and Subject Alternative Names from a
// CSR, and returns a slice of ACMEIdentifiers.
func FromCSR(csr *x509.CertificateRequest) []ACMEIdentifier {
	return fromX509(csr.Subject.CommonName, csr.DNSNames, csr.IPAddresses)
}

// Normalize returns the set of all unique ACME identifiers in the input after
// all of them are lowercased. The returned identifier values will be in their
// lowercased form and sorted alphabetically by value. DNS identifiers will
// precede IP address identifiers.
func Normalize(idents []ACMEIdentifier) []ACMEIdentifier {
	for i := range idents {
		idents[i].Value = strings.ToLower(idents[i].Value)
	}

	slices.SortFunc(idents, func(a, b ACMEIdentifier) int {
		if a.Type == b.Type {
			if a.Value == b.Value {
				return 0
			}
			if a.Value < b.Value {
				return -1
			}
			return 1
		}
		if a.Type == "dns" && b.Type == "ip" {
			return -1
		}
		return 1
	})

	return slices.Compact(idents)
}
