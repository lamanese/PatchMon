// Package branding holds the user-facing product name of the fork.
//
// The name is deliberately one set of constants so that a rename touches this
// file only. Technical identifiers (agent binary name, service names, config
// paths, the PM_ environment prefix, the X-PatchMon-Signature webhook header,
// the "PatchMon Agent v" banner parsed by the server) are NOT derived from it
// and must not be changed here.
package branding

const (
	// ProductName is the full product name shown in titles, mails and PDFs.
	ProductName = "amanIT PatMan"
	// ProductNameShort is the short form used where space is tight.
	ProductNameShort = "PatMan"
	// VendorName is the operator named in footers and as PDF author.
	VendorName = "amanIT GmbH"
	// VendorURL is linked from footers.
	VendorURL = "https://amanit.swiss"
)
