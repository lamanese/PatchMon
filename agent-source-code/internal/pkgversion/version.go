// Package pkgversion provides version information for the agent
package pkgversion

// Version represents the current version of the patchmon-agent.
// Bumped in this fork (upstream 2.0.2) so the server reports a newer bundled
// agent than what is installed on hosts, which is what triggers the agent
// self-update - otherwise agent-side fixes never reach already-enrolled hosts.
const Version = "2.0.4"
