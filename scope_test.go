package budgetengine

import "testing"

// TestScope_KeyContract pins the partition-key format to tenant/project/pi/grant (Affiliation and
// AccountID excluded), byte-identical to seam.Principal.Key(). This mirrors prp's parity test so the
// engine's Scope can never silently drift from the seam contract the hosts share. Pure string test,
// no host import.
func TestScope_KeyContract(t *testing.T) {
	cases := []struct {
		s    Scope
		want string
	}{
		{Scope{Tenant: "harvard", Project: "prism", PI: "curie", Grant: "nsf-123"}, "harvard/prism/curie/nsf-123"},
		{Scope{Tenant: "harvard"}, "harvard///"},
		{Scope{}, "///"},
		// Affiliation and AccountID must NOT appear in the key.
		{Scope{Tenant: "h", PI: "p", Affiliation: "faculty", AccountID: "111"}, "h//p/"},
	}
	for _, c := range cases {
		if got := c.s.Key(); got != c.want {
			t.Errorf("Scope(%+v).Key() = %q, want %q", c.s, got, c.want)
		}
	}
}
