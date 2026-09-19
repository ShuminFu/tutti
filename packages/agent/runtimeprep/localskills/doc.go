// Package localskills discovers the SKILL.md skills a user keeps next to a
// workspace and in their home directory, without going through a provider
// plugin registry.
//
// A skill is a directory holding a SKILL.md whose YAML frontmatter carries a
// name and a description. StandardRoots derives the candidate roots for one
// session, Discover reads them in order, Read parses a single file, and Filter
// narrows a catalog to an explicit selection.
//
// Two container layouts are recognized under an ancestor directory:
//
//	<ancestor>/.agents/skills/<name>/SKILL.md
//	<ancestor>/.agents/<name>/SKILL.md
//
// Nothing below a root is walked, so an unrelated nested .agents directory is
// never picked up, and hidden entries are never scanned.
//
// The package keeps no global cache. Every call re-reads the filesystem, so a
// skill edited between two launches is visible to the next one.
package localskills
