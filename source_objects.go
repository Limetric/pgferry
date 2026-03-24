package main

import "fmt"

// SourceView is a source-database view that may include an informational source definition.
type SourceView struct {
	Name       string
	Dialect    string
	Definition string
}

// SourceRoutine is a source-database routine that may include an informational source definition.
type SourceRoutine struct {
	Name       string
	Type       string
	Dialect    string
	Definition string
}

func (r SourceRoutine) DisplayName() string {
	if r.Type == "" {
		return r.Name
	}
	return fmt.Sprintf("%s %s", r.Type, r.Name)
}

// SourceTrigger is a source-database trigger attached to a specific table.
type SourceTrigger struct {
	Name       string
	Table      string // source table the trigger is on (empty if unknown)
	Dialect    string
	Definition string
}

// SourceObjects holds non-table source objects that require manual migration.
type SourceObjects struct {
	Views    []SourceView
	Routines []SourceRoutine
	Triggers []SourceTrigger
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
		warnings = append(warnings, fmt.Sprintf("view: %s", v.Name))
	}
	for _, r := range objs.Routines {
		warnings = append(warnings, fmt.Sprintf("routine: %s", r.DisplayName()))
	}
	for _, t := range objs.Triggers {
		if t.Table != "" {
			warnings = append(warnings, fmt.Sprintf("trigger: %s (on %s)", t.Name, t.Table))
		} else {
			warnings = append(warnings, fmt.Sprintf("trigger: %s", t.Name))
		}
	}
	return warnings
}
