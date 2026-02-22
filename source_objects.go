package main

import (
	"database/sql"
	"fmt"
	"strings"
)

type SourceObjects struct {
	Views    []string
	Routines []string
	Triggers []string
}

func introspectSourceObjects(db *sql.DB, dbName string) (*SourceObjects, error) {
	objs := &SourceObjects{}

	if err := collectStringRows(db, `
		SELECT TABLE_NAME
		FROM INFORMATION_SCHEMA.VIEWS
		WHERE TABLE_SCHEMA = ?
		ORDER BY TABLE_NAME
	`, dbName, &objs.Views); err != nil {
		return nil, fmt.Errorf("introspect views: %w", err)
	}

	rows, err := db.Query(`
		SELECT ROUTINE_TYPE, ROUTINE_NAME
		FROM INFORMATION_SCHEMA.ROUTINES
		WHERE ROUTINE_SCHEMA = ?
		ORDER BY ROUTINE_TYPE, ROUTINE_NAME
	`, dbName)
	if err != nil {
		return nil, fmt.Errorf("introspect routines: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var routineType, routineName string
		if err := rows.Scan(&routineType, &routineName); err != nil {
			return nil, fmt.Errorf("scan routines: %w", err)
		}
		objs.Routines = append(objs.Routines, fmt.Sprintf("%s %s", strings.ToUpper(routineType), routineName))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate routines: %w", err)
	}

	if err := collectStringRows(db, `
		SELECT TRIGGER_NAME
		FROM INFORMATION_SCHEMA.TRIGGERS
		WHERE TRIGGER_SCHEMA = ?
		ORDER BY TRIGGER_NAME
	`, dbName, &objs.Triggers); err != nil {
		return nil, fmt.Errorf("introspect triggers: %w", err)
	}

	return objs, nil
}

func collectStringRows(db *sql.DB, query, dbName string, out *[]string) error {
	rows, err := db.Query(query, dbName)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return err
		}
		*out = append(*out, v)
	}
	return rows.Err()
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
