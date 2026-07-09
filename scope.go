package budgetengine

import "strings"

// Scope is the engine's own tenancy/identity key. It is contractually IDENTICAL to Prism's and
// prp's seam.Principal — same field set, same order, same Key() — so a host can convert between the
// two with a plain struct conversion (budgetengine.Scope(principal)) and no adapter. The engine
// never imports a host's seam package; keeping the layout identical is what lets it stay
// dependency-free while remaining wire-compatible.
//
// A record is partitioned by Scope. Key() joins tenant/project/pi/grant in a fixed order;
// Affiliation and AccountID are deliberately NOT part of the key (Affiliation is a derived tier, and
// a record's home does not move when acted on from a different account). This MUST stay identical to
// seam.Principal.Key().
type Scope struct {
	Tenant      string
	Project     string
	PI          string
	Grant       string
	Affiliation string
	AccountID   string
}

// Key is the deterministic partition key for a scope.
func (s Scope) Key() string {
	return strings.Join([]string{s.Tenant, s.Project, s.PI, s.Grant}, "/")
}
