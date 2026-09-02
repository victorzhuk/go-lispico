package inventory

// FamilyMigrated tracks which families have been migrated. A seam flips its own
// family once its rows land. "support" starts true: nothing in it needs
// migrating.
var FamilyMigrated = map[string]bool{
	"numeric":      true,
	"types":        true,
	"collection":   false,
	"higher-order": false,
	"string":       false,
	"cl-adapter":   false,
	"support":      true,
}
