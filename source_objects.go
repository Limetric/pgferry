package main

import "fmt"

// SourceObjects holds non-table source objects that require manual migration.
type SourceObjects struct {
	Views    []string
	Routines []string
	Triggers []string
}

func sourceObjectWarnings(objs *SourceObjects) []string {
	if objs == nil {
		return nil
	}

	var warnings []string
	if len(objs.Views) == 0 && len(objs.Routines) == 0 && len(objs.Triggers) == 0 {
		return warnings
	}

	warnings = append(warnings,
		fmt.Sprintf(
			"source contains non-table objects not migrated automatically (%d views, %d routines, %d triggers)",
			len(objs.Views), len(objs.Routines), len(objs.Triggers),
		),
	)
	for _, v := range objs.Views {
		warnings = append(warnings, fmt.Sprintf("view: %s", v))
	}
	for _, r := range objs.Routines {
		warnings = append(warnings, fmt.Sprintf("routine: %s", r))
	}
	for _, t := range objs.Triggers {
		warnings = append(warnings, fmt.Sprintf("trigger: %s", t))
	}
	return warnings
}
