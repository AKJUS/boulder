package policy

import (
	"fmt"
	"net/netip"
	"os"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/letsencrypt/boulder/core"
	berrors "github.com/letsencrypt/boulder/errors"
	"github.com/letsencrypt/boulder/features"
	"github.com/letsencrypt/boulder/identifier"
	blog "github.com/letsencrypt/boulder/log"
	"github.com/letsencrypt/boulder/test"
)

func paImpl(t *testing.T) *AuthorityImpl {
	enabledChallenges := map[core.AcmeChallenge]bool{
		core.ChallengeTypeHTTP01:    true,
		core.ChallengeTypeDNS01:     true,
		core.ChallengeTypeTLSALPN01: true,
	}

	pa, err := New(enabledChallenges, blog.NewMock())
	if err != nil {
		t.Fatalf("Couldn't create policy implementation: %s", err)
	}
	return pa
}

func TestWellFormedIdentifiers(t *testing.T) {
	testCases := []struct {
		domain string
		err    error
	}{
		{``, errEmptyName},                    // Empty name
		{`zomb!.com`, errInvalidDNSCharacter}, // ASCII character out of range
		{`emailaddress@myseriously.present.com`, errInvalidDNSCharacter},
		{`user:pass@myseriously.present.com`, errInvalidDNSCharacter},
		{`zömbo.com`, errInvalidDNSCharacter},                              // non-ASCII character
		{`127.0.0.1`, errIPAddress},                                        // IPv4 address
		{`fe80::1:1`, errInvalidDNSCharacter},                              // IPv6 addresses
		{`[2001:db8:85a3:8d3:1319:8a2e:370:7348]`, errInvalidDNSCharacter}, // unexpected IPv6 variants
		{`[2001:db8:85a3:8d3:1319:8a2e:370:7348]:443`, errInvalidDNSCharacter},
		{`2001:db8::/32`, errInvalidDNSCharacter},
		{`a.b.c.d.e.f.g.h.i.j.k`, errTooManyLabels}, // Too many labels (>10)

		{`www.0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef012345.com`, errNameTooLong}, // Too long (254 characters)

		{`www.ef0123456789abcdef013456789abcdef012345.789abcdef012345679abcdef0123456789abcdef01234.6789abcdef0123456789abcdef0.23456789abcdef0123456789a.cdef0123456789abcdef0123456789ab.def0123456789abcdef0123456789.bcdef0123456789abcdef012345.com`, nil}, // OK, not too long (240 characters)

		{`www.abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz.com`, errLabelTooLong}, // Label too long (>63 characters)

		{`www.-ombo.com`, errInvalidDNSCharacter}, // Label starts with '-'
		{`www.zomb-.com`, errInvalidDNSCharacter}, // Label ends with '-'
		{`xn--.net`, errInvalidDNSCharacter},      // Label ends with '-'
		{`-0b.net`, errInvalidDNSCharacter},       // First label begins with '-'
		{`-0.net`, errInvalidDNSCharacter},        // First label begins with '-'
		{`-.net`, errInvalidDNSCharacter},         // First label is only '-'
		{`---.net`, errInvalidDNSCharacter},       // First label is only hyphens
		{`0`, errTooFewLabels},
		{`1`, errTooFewLabels},
		{`*`, errMalformedWildcard},
		{`**`, errTooManyWildcards},
		{`*.*`, errTooManyWildcards},
		{`zombo*com`, errMalformedWildcard},
		{`*.com`, errICANNTLDWildcard},
		{`..a`, errLabelTooShort},
		{`a..a`, errLabelTooShort},
		{`.a..a`, errLabelTooShort},
		{`..foo.com`, errLabelTooShort},
		{`.`, errNameEndsInDot},
		{`..`, errNameEndsInDot},
		{`a..`, errNameEndsInDot},
		{`.....`, errNameEndsInDot},
		{`.a.`, errNameEndsInDot},
		{`www.zombo.com.`, errNameEndsInDot},
		{`www.zombo_com.com`, errInvalidDNSCharacter},
		{`\uFEFF`, errInvalidDNSCharacter}, // Byte order mark
		{`\uFEFFwww.zombo.com`, errInvalidDNSCharacter},
		{`www.zom\u202Ebo.com`, errInvalidDNSCharacter}, // Right-to-Left Override
		{`\u202Ewww.zombo.com`, errInvalidDNSCharacter},
		{`www.zom\u200Fbo.com`, errInvalidDNSCharacter}, // Right-to-Left Mark
		{`\u200Fwww.zombo.com`, errInvalidDNSCharacter},
		// Underscores are technically disallowed in DNS. Some DNS
		// implementations accept them but we will be conservative.
		{`www.zom_bo.com`, errInvalidDNSCharacter},
		{`zombocom`, errTooFewLabels},
		{`localhost`, errTooFewLabels},
		{`mail`, errTooFewLabels},

		// disallow capitalized letters for #927
		{`CapitalizedLetters.com`, errInvalidDNSCharacter},

		{`example.acting`, errNonPublic},
		{`example.internal`, errNonPublic},
		// All-numeric final label not okay.
		{`www.zombo.163`, errNonPublic},
		{`xn--109-3veba6djs1bfxlfmx6c9g.xn--f1awi.xn--p1ai`, errMalformedIDN}, // Not in Unicode NFC
		{`bq--abwhky3f6fxq.jakacomo.com`, errInvalidRLDH},
		// Three hyphens starting at third second char of first label.
		{`bq---abwhky3f6fxq.jakacomo.com`, errInvalidRLDH},
		// Three hyphens starting at second char of first label.
		{`h---test.hk2yz.org`, errInvalidRLDH},
		{`co.uk`, errICANNTLD},
		{`foo.bd`, errICANNTLD},
	}

	// Test syntax errors
	for _, tc := range testCases {
		err := WellFormedIdentifiers(identifier.ACMEIdentifiers{identifier.NewDNS(tc.domain)})
		if tc.err == nil {
			test.AssertNil(t, err, fmt.Sprintf("Unexpected error for domain %q, got %s", tc.domain, err))
		} else {
			test.AssertError(t, err, fmt.Sprintf("Expected error for domain %q, but got none", tc.domain))
			var berr *berrors.BoulderError
			test.AssertErrorWraps(t, err, &berr)
			test.AssertContains(t, berr.Error(), tc.err.Error())
		}
	}

	err := WellFormedIdentifiers(identifier.ACMEIdentifiers{identifier.NewIP(netip.MustParseAddr("9.9.9.9"))})
	test.AssertError(t, err, "Expected error for IP, but got none")
	var berr *berrors.BoulderError
	test.AssertErrorWraps(t, err, &berr)
	test.AssertContains(t, berr.Error(), errUnsupportedIdent.Error())
}

func TestWillingToIssue(t *testing.T) {
	shouldBeBlocked := []string{
		`highvalue.website1.org`,
		`website2.co.uk`,
		`www.website3.com`,
		`lots.of.labels.website4.com`,
		`banned.in.dc.com`,
		`bad.brains.banned.in.dc.com`,
	}
	blocklistContents := []string{
		`website2.com`,
		`website2.org`,
		`website2.co.uk`,
		`website3.com`,
		`website4.com`,
	}
	exactBlocklistContents := []string{
		`www.website1.org`,
		`highvalue.website1.org`,
		`dl.website1.org`,
	}
	adminBlockedContents := []string{
		`banned.in.dc.com`,
	}

	shouldBeAccepted := []string{
		`lowvalue.website1.org`,
		`website4.sucks`,
		"www.unrelated.com",
		"unrelated.com",
		"www.8675309.com",
		"8675309.com",
		"web5ite2.com",
		"www.web-site2.com",
	}

	policy := blockedNamesPolicy{
		HighRiskBlockedNames: blocklistContents,
		ExactBlockedNames:    exactBlocklistContents,
		AdminBlockedNames:    adminBlockedContents,
	}

	yamlPolicyBytes, err := yaml.Marshal(policy)
	test.AssertNotError(t, err, "Couldn't YAML serialize blocklist")
	yamlPolicyFile, _ := os.CreateTemp("", "test-blocklist.*.yaml")
	defer os.Remove(yamlPolicyFile.Name())
	err = os.WriteFile(yamlPolicyFile.Name(), yamlPolicyBytes, 0640)
	test.AssertNotError(t, err, "Couldn't write YAML blocklist")

	pa := paImpl(t)

	err = pa.LoadHostnamePolicyFile(yamlPolicyFile.Name())
	test.AssertNotError(t, err, "Couldn't load rules")

	// Invalid encoding
	err = pa.WillingToIssue(identifier.ACMEIdentifiers{identifier.NewDNS("www.xn--m.com")})
	test.AssertError(t, err, "WillingToIssue didn't fail on a malformed IDN")
	// Invalid identifier type
	err = pa.WillingToIssue(identifier.ACMEIdentifiers{identifier.NewIP(netip.MustParseAddr("1.1.1.1"))})
	test.AssertError(t, err, "WillingToIssue didn't fail on an IP address")
	// Valid encoding
	err = pa.WillingToIssue(identifier.ACMEIdentifiers{identifier.NewDNS("www.xn--mnich-kva.com")})
	test.AssertNotError(t, err, "WillingToIssue failed on a properly formed IDN")
	// IDN TLD
	err = pa.WillingToIssue(identifier.ACMEIdentifiers{identifier.NewDNS("xn--example--3bhk5a.xn--p1ai")})
	test.AssertNotError(t, err, "WillingToIssue failed on a properly formed domain with IDN TLD")
	features.Reset()

	// Test expected blocked domains
	for _, domain := range shouldBeBlocked {
		err := pa.WillingToIssue(identifier.ACMEIdentifiers{identifier.NewDNS(domain)})
		test.AssertError(t, err, "domain was not correctly forbidden")
		var berr *berrors.BoulderError
		test.AssertErrorWraps(t, err, &berr)
		test.AssertContains(t, berr.Detail, errPolicyForbidden.Error())
	}

	// Test acceptance of good names
	for _, domain := range shouldBeAccepted {
		err := pa.WillingToIssue(identifier.ACMEIdentifiers{identifier.NewDNS(domain)})
		test.AssertNotError(t, err, "domain was incorrectly forbidden")
	}
}

func TestWillingToIssue_Wildcards(t *testing.T) {
	bannedDomains := []string{
		"zombo.gov.us",
	}
	exactBannedDomains := []string{
		"highvalue.letsdecrypt.org",
	}
	pa := paImpl(t)

	bannedBytes, err := yaml.Marshal(blockedNamesPolicy{
		HighRiskBlockedNames: bannedDomains,
		ExactBlockedNames:    exactBannedDomains,
	})
	test.AssertNotError(t, err, "Couldn't serialize banned list")
	f, _ := os.CreateTemp("", "test-wildcard-banlist.*.yaml")
	defer os.Remove(f.Name())
	err = os.WriteFile(f.Name(), bannedBytes, 0640)
	test.AssertNotError(t, err, "Couldn't write serialized banned list to file")
	err = pa.LoadHostnamePolicyFile(f.Name())
	test.AssertNotError(t, err, "Couldn't load policy contents from file")

	testCases := []struct {
		Name        string
		Domain      string
		ExpectedErr error
	}{
		{
			Name:        "Too many wildcards",
			Domain:      "ok.*.whatever.*.example.com",
			ExpectedErr: errTooManyWildcards,
		},
		{
			Name:        "Misplaced wildcard",
			Domain:      "ok.*.whatever.example.com",
			ExpectedErr: errMalformedWildcard,
		},
		{
			Name:        "Missing ICANN TLD",
			Domain:      "*.ok.madeup",
			ExpectedErr: errNonPublic,
		},
		{
			Name:        "Wildcard for ICANN TLD",
			Domain:      "*.com",
			ExpectedErr: errICANNTLDWildcard,
		},
		{
			Name:        "Forbidden base domain",
			Domain:      "*.zombo.gov.us",
			ExpectedErr: errPolicyForbidden,
		},
		// We should not allow getting a wildcard for that would cover an exact
		// blocklist domain
		{
			Name:        "Wildcard for ExactBlocklist base domain",
			Domain:      "*.letsdecrypt.org",
			ExpectedErr: errPolicyForbidden,
		},
		// We should allow a wildcard for a domain that doesn't match the exact
		// blocklist domain
		{
			Name:        "Wildcard for non-matching subdomain of ExactBlocklist domain",
			Domain:      "*.lowvalue.letsdecrypt.org",
			ExpectedErr: nil,
		},
		// We should allow getting a wildcard for an exact blocklist domain since it
		// only covers subdomains, not the exact name.
		{
			Name:        "Wildcard for ExactBlocklist domain",
			Domain:      "*.highvalue.letsdecrypt.org",
			ExpectedErr: nil,
		},
		{
			Name:        "Valid wildcard domain",
			Domain:      "*.everything.is.possible.at.zombo.com",
			ExpectedErr: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			err := pa.WillingToIssue(identifier.ACMEIdentifiers{identifier.NewDNS(tc.Domain)})
			if tc.ExpectedErr == nil {
				test.AssertNil(t, err, fmt.Sprintf("Unexpected error for domain %q, got %s", tc.Domain, err))
			} else {
				test.AssertError(t, err, fmt.Sprintf("Expected error for domain %q, but got none", tc.Domain))
				var berr *berrors.BoulderError
				test.AssertErrorWraps(t, err, &berr)
				test.AssertContains(t, berr.Error(), tc.ExpectedErr.Error())
			}
		})
	}
}

// TestWillingToIssue_SubErrors tests that more than one rejected identifier
// results in an error with suberrors.
func TestWillingToIssue_SubErrors(t *testing.T) {
	banned := []string{
		"letsdecrypt.org",
		"example.com",
	}
	pa := paImpl(t)

	bannedBytes, err := yaml.Marshal(blockedNamesPolicy{
		HighRiskBlockedNames: banned,
		ExactBlockedNames:    banned,
	})
	test.AssertNotError(t, err, "Couldn't serialize banned list")
	f, _ := os.CreateTemp("", "test-wildcard-banlist.*.yaml")
	defer os.Remove(f.Name())
	err = os.WriteFile(f.Name(), bannedBytes, 0640)
	test.AssertNotError(t, err, "Couldn't write serialized banned list to file")
	err = pa.LoadHostnamePolicyFile(f.Name())
	test.AssertNotError(t, err, "Couldn't load policy contents from file")

	// Test multiple malformed domains and one banned domain; only the malformed ones will generate errors
	err = pa.WillingToIssue(identifier.ACMEIdentifiers{
		identifier.NewDNS("perfectly-fine.com"),      // fine
		identifier.NewDNS("letsdecrypt_org"),         // malformed
		identifier.NewDNS("example.comm"),            // malformed
		identifier.NewDNS("letsdecrypt.org"),         // banned
		identifier.NewDNS("also-perfectly-fine.com"), // fine
	})
	test.AssertDeepEquals(t, err,
		&berrors.BoulderError{
			Type:   berrors.RejectedIdentifier,
			Detail: "Cannot issue for \"letsdecrypt_org\": Domain name contains an invalid character (and 1 more problems. Refer to sub-problems for more information.)",
			SubErrors: []berrors.SubBoulderError{
				{
					BoulderError: &berrors.BoulderError{
						Type:   berrors.Malformed,
						Detail: "Domain name contains an invalid character",
					},
					Identifier: identifier.NewDNS("letsdecrypt_org"),
				},
				{
					BoulderError: &berrors.BoulderError{
						Type:   berrors.Malformed,
						Detail: "Domain name does not end with a valid public suffix (TLD)",
					},
					Identifier: identifier.NewDNS("example.comm"),
				},
			},
		})

	// Test multiple banned domains.
	err = pa.WillingToIssue(identifier.ACMEIdentifiers{
		identifier.NewDNS("perfectly-fine.com"),      // fine
		identifier.NewDNS("letsdecrypt.org"),         // banned
		identifier.NewDNS("example.com"),             // banned
		identifier.NewDNS("also-perfectly-fine.com"), // fine
	})
	test.AssertError(t, err, "Expected err from WillingToIssueWildcards")

	test.AssertDeepEquals(t, err,
		&berrors.BoulderError{
			Type:   berrors.RejectedIdentifier,
			Detail: "Cannot issue for \"letsdecrypt.org\": The ACME server refuses to issue a certificate for this domain name, because it is forbidden by policy (and 1 more problems. Refer to sub-problems for more information.)",
			SubErrors: []berrors.SubBoulderError{
				{
					BoulderError: &berrors.BoulderError{
						Type:   berrors.RejectedIdentifier,
						Detail: "The ACME server refuses to issue a certificate for this domain name, because it is forbidden by policy",
					},
					Identifier: identifier.NewDNS("letsdecrypt.org"),
				},
				{
					BoulderError: &berrors.BoulderError{
						Type:   berrors.RejectedIdentifier,
						Detail: "The ACME server refuses to issue a certificate for this domain name, because it is forbidden by policy",
					},
					Identifier: identifier.NewDNS("example.com"),
				},
			},
		})

	// Test willing to issue with only *one* bad identifier.
	err = pa.WillingToIssue(identifier.ACMEIdentifiers{identifier.NewDNS("letsdecrypt.org")})
	test.AssertDeepEquals(t, err,
		&berrors.BoulderError{
			Type:   berrors.RejectedIdentifier,
			Detail: "Cannot issue for \"letsdecrypt.org\": The ACME server refuses to issue a certificate for this domain name, because it is forbidden by policy",
		})
}

func TestChallengeTypesFor(t *testing.T) {
	t.Parallel()
	pa := paImpl(t)

	testCases := []struct {
		name       string
		ident      identifier.ACMEIdentifier
		wantChalls []core.AcmeChallenge
		wantErr    string
	}{
		{
			name:  "dns",
			ident: identifier.NewDNS("example.com"),
			wantChalls: []core.AcmeChallenge{
				core.ChallengeTypeHTTP01, core.ChallengeTypeDNS01, core.ChallengeTypeTLSALPN01,
			},
		},
		{
			name:  "wildcard",
			ident: identifier.NewDNS("*.example.com"),
			wantChalls: []core.AcmeChallenge{
				core.ChallengeTypeDNS01,
			},
		},
		{
			name:    "other",
			ident:   identifier.NewIP(netip.MustParseAddr("1.2.3.4")),
			wantErr: "unrecognized identifier type",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			challs, err := pa.ChallengeTypesFor(tc.ident)

			if len(tc.wantChalls) != 0 {
				test.AssertNotError(t, err, "should have succeeded")
				test.AssertDeepEquals(t, challs, tc.wantChalls)
			}

			if tc.wantErr != "" {
				test.AssertError(t, err, "should have errored")
				test.AssertContains(t, err.Error(), tc.wantErr)
			}
		})
	}
}

// TestMalformedExactBlocklist tests that loading a YAML policy file with an
// invalid exact blocklist entry will fail as expected.
func TestMalformedExactBlocklist(t *testing.T) {
	pa := paImpl(t)

	exactBannedDomains := []string{
		// Only one label - not valid
		"com",
	}
	bannedDomains := []string{
		"placeholder.domain.not.important.for.this.test.com",
	}

	// Create YAML for the exactBannedDomains
	bannedBytes, err := yaml.Marshal(blockedNamesPolicy{
		HighRiskBlockedNames: bannedDomains,
		ExactBlockedNames:    exactBannedDomains,
	})
	test.AssertNotError(t, err, "Couldn't serialize banned list")

	// Create a temp file for the YAML contents
	f, _ := os.CreateTemp("", "test-invalid-exactblocklist.*.yaml")
	defer os.Remove(f.Name())
	// Write the YAML to the temp file
	err = os.WriteFile(f.Name(), bannedBytes, 0640)
	test.AssertNotError(t, err, "Couldn't write serialized banned list to file")

	// Try to use the YAML tempfile as the hostname policy. It should produce an
	// error since the exact blocklist contents are malformed.
	err = pa.LoadHostnamePolicyFile(f.Name())
	test.AssertError(t, err, "Loaded invalid exact blocklist content without error")
	test.AssertEquals(t, err.Error(), "Malformed ExactBlockedNames entry, only one label: \"com\"")
}

func TestValidEmailError(t *testing.T) {
	err := ValidEmail("(๑•́ ω •̀๑)")
	test.AssertEquals(t, err.Error(), "unable to parse email address")

	err = ValidEmail("john.smith@gmail.com #replace with real email")
	test.AssertEquals(t, err.Error(), "unable to parse email address")

	err = ValidEmail("example@example.com")
	test.AssertEquals(t, err.Error(), "contact email has forbidden domain \"example.com\"")

	err = ValidEmail("example@-foobar.com")
	test.AssertEquals(t, err.Error(), "contact email has invalid domain: Domain name contains an invalid character")
}

func TestCheckAuthzChallenges(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		authz   core.Authorization
		enabled map[core.AcmeChallenge]bool
		wantErr string
	}{
		{
			name: "unrecognized identifier",
			authz: core.Authorization{
				Identifier: identifier.ACMEIdentifier{Type: "oops", Value: "example.com"},
				Challenges: []core.Challenge{{Type: core.ChallengeTypeDNS01, Status: core.StatusValid}},
			},
			wantErr: "unrecognized identifier type",
		},
		{
			name: "no challenges",
			authz: core.Authorization{
				Identifier: identifier.NewDNS("example.com"),
				Challenges: []core.Challenge{},
			},
			wantErr: "has no challenges",
		},
		{
			name: "no valid challenges",
			authz: core.Authorization{
				Identifier: identifier.NewDNS("example.com"),
				Challenges: []core.Challenge{{Type: core.ChallengeTypeDNS01, Status: core.StatusPending}},
			},
			wantErr: "not solved by any challenge",
		},
		{
			name: "solved by disabled challenge",
			authz: core.Authorization{
				Identifier: identifier.NewDNS("example.com"),
				Challenges: []core.Challenge{{Type: core.ChallengeTypeDNS01, Status: core.StatusValid}},
			},
			enabled: map[core.AcmeChallenge]bool{core.ChallengeTypeHTTP01: true},
			wantErr: "disabled challenge type",
		},
		{
			name: "solved by wrong kind of challenge",
			authz: core.Authorization{
				Identifier: identifier.NewDNS("*.example.com"),
				Challenges: []core.Challenge{{Type: core.ChallengeTypeHTTP01, Status: core.StatusValid}},
			},
			wantErr: "inapplicable challenge type",
		},
		{
			name: "valid authz",
			authz: core.Authorization{
				Identifier: identifier.NewDNS("example.com"),
				Challenges: []core.Challenge{{Type: core.ChallengeTypeTLSALPN01, Status: core.StatusValid}},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pa := paImpl(t)

			if tc.enabled != nil {
				pa.enabledChallenges = tc.enabled
			}

			err := pa.CheckAuthzChallenges(&tc.authz)

			if tc.wantErr == "" {
				test.AssertNotError(t, err, "should have succeeded")
			} else {
				test.AssertError(t, err, "should have errored")
				test.AssertContains(t, err.Error(), tc.wantErr)
			}
		})
	}
}
