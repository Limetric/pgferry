package main

import "fmt"

const temporalExamplesLimit = 3

func collectTemporalWarnings(schema *Schema, sourceType string, typeMap TypeMappingConfig) []PlanTemporalWarning {
	if schema == nil {
		return []PlanTemporalWarning{}
	}

	switch sourceType {
	case "mysql":
		return collectMySQLTemporalWarnings(schema, typeMap)
	case "mssql":
		return collectMSSQLTemporalWarnings(schema, typeMap)
	default:
		return []PlanTemporalWarning{}
	}
}

func collectMySQLTemporalWarnings(schema *Schema, typeMap TypeMappingConfig) []PlanTemporalWarning {
	var warnings []PlanTemporalWarning
	var timeCols []string
	var dateLikeCols []string
	var datetimeCols []string
	var timestampCols []string

	for _, table := range schema.Tables {
		for _, col := range table.Columns {
			ref := temporalColumnRef(table, col)
			switch col.DataType {
			case "time":
				timeCols = append(timeCols, ref)
			case "date":
				dateLikeCols = append(dateLikeCols, ref)
			case "datetime":
				dateLikeCols = append(dateLikeCols, ref)
				datetimeCols = append(datetimeCols, ref)
			case "timestamp":
				dateLikeCols = append(dateLikeCols, ref)
				timestampCols = append(timestampCols, ref)
			}
		}
	}

	switch typeMap.TimeMode {
	case "time":
		if len(timeCols) > 0 {
			warnings = append(warnings, newTemporalWarning(
				"mysql_time_mode_time",
				timeCols,
				fmt.Sprintf("%d MySQL TIME column(s) will map to PostgreSQL time; negative durations or values outside 00:00:00-23:59:59 can fail or drift semantically.", len(timeCols)),
				`Use type_mapping.time_mode = "interval" for durations or "text" to preserve source literals exactly.`,
			))
		}
	case "interval":
		if len(timeCols) > 0 {
			warnings = append(warnings, newTemporalWarning(
				"mysql_time_mode_interval",
				timeCols,
				fmt.Sprintf("%d MySQL TIME column(s) will map to PostgreSQL interval; review whether these columns represent durations rather than wall-clock times.", len(timeCols)),
				`Use type_mapping.time_mode = "time" for clock-time values or "text" to preserve the original representation.`,
			))
		}
	}

	if typeMap.ZeroDateMode == "null" && len(dateLikeCols) > 0 {
		warnings = append(warnings, newTemporalWarning(
			"mysql_zero_date_mode_null",
			dateLikeCols,
			fmt.Sprintf("%d MySQL date/datetime/timestamp column(s) use zero_date_mode = \"null\"; any zero dates in source data will become NULL in PostgreSQL.", len(dateLikeCols)),
			`Set type_mapping.zero_date_mode = "error" to fail fast on zero dates, or confirm that converting them to NULL is acceptable.`,
		))
	}

	if !typeMap.DatetimeAsTimestamptz && len(datetimeCols) > 0 {
		warnings = append(warnings, newTemporalWarning(
			"mysql_datetime_without_timezone",
			datetimeCols,
			fmt.Sprintf("%d MySQL datetime column(s) will map to PostgreSQL timestamp without timezone semantics; review whether type_mapping.datetime_as_timestamptz = true is more appropriate.", len(datetimeCols)),
			`Use type_mapping.datetime_as_timestamptz = true when those values represent real instants instead of local wall-clock timestamps.`,
		))
	}
	if typeMap.DatetimeAsTimestamptz && len(datetimeCols) > 0 {
		warnings = append(warnings, newTemporalWarning(
			"mysql_datetime_to_timestamptz",
			datetimeCols,
			fmt.Sprintf("%d MySQL datetime column(s) will map to PostgreSQL timestamptz; confirm these values represent real instants rather than local wall-clock timestamps.", len(datetimeCols)),
			`If these values should stay timezone-naive wall-clock timestamps, keep type_mapping.datetime_as_timestamptz = false instead.`,
		))
	}

	if len(timestampCols) > 0 {
		warnings = append(warnings, newTemporalWarning(
			"mysql_timestamp_to_timestamptz",
			timestampCols,
			fmt.Sprintf("%d MySQL timestamp column(s) will map to PostgreSQL timestamptz; review application and session timezone assumptions in the target stack.", len(timestampCols)),
			`No alternate mapping exists for MySQL timestamp; verify client time zone settings if these columns drive user-visible timestamps, and ignore this warning if that behavior is intentional.`,
		))
	}

	return warnings
}

func collectMSSQLTemporalWarnings(schema *Schema, typeMap TypeMappingConfig) []PlanTemporalWarning {
	var warnings []PlanTemporalWarning
	var datetimeLikeCols []string
	var datetimeOffsetCols []string

	for _, table := range schema.Tables {
		for _, col := range table.Columns {
			ref := temporalColumnRef(table, col)
			switch col.DataType {
			case "datetime", "datetime2", "smalldatetime":
				datetimeLikeCols = append(datetimeLikeCols, ref)
			case "datetimeoffset":
				datetimeOffsetCols = append(datetimeOffsetCols, ref)
			}
		}
	}

	if len(datetimeLikeCols) > 0 {
		summary := fmt.Sprintf("%d MSSQL datetime/datetime2/smalldatetime column(s) will map to PostgreSQL timestamp without timezone semantics; review whether type_mapping.datetime_as_timestamptz = true is more appropriate. Note: smalldatetime is only precise to the minute.", len(datetimeLikeCols))
		remediation := `Use type_mapping.datetime_as_timestamptz = true when those values represent real instants instead of local wall-clock timestamps.`
		category := "mssql_datetime_without_timezone"
		if typeMap.DatetimeAsTimestamptz {
			summary = fmt.Sprintf("%d MSSQL datetime/datetime2/smalldatetime column(s) will map to PostgreSQL timestamptz; confirm these values represent real instants rather than local wall-clock timestamps. Note: smalldatetime is only precise to the minute.", len(datetimeLikeCols))
			remediation = `If these values should stay timezone-naive wall-clock timestamps, keep type_mapping.datetime_as_timestamptz = false instead.`
			category = "mssql_datetime_to_timestamptz"
		}
		warnings = append(warnings, newTemporalWarning(category, datetimeLikeCols, summary, remediation))
	}

	if len(datetimeOffsetCols) > 0 {
		warnings = append(warnings, newTemporalWarning(
			"mssql_datetimeoffset_to_timestamptz",
			datetimeOffsetCols,
			fmt.Sprintf("%d MSSQL datetimeoffset column(s) will map to PostgreSQL timestamptz; confirm clients and downstream queries handle offset-normalized values as expected.", len(datetimeOffsetCols)),
			`Review application behavior around time zone display and comparisons for datetimeoffset-derived data.`,
		))
	}

	return warnings
}

func newTemporalWarning(category string, refs []string, summary, remediation string) PlanTemporalWarning {
	n := len(refs)
	if n > temporalExamplesLimit {
		n = temporalExamplesLimit
	}
	examples := make([]string, n)
	copy(examples, refs[:n])
	return PlanTemporalWarning{
		Category:    category,
		Summary:     summary,
		Columns:     len(refs),
		Examples:    examples,
		Remediation: remediation,
	}
}

func temporalColumnRef(table Table, col Column) string {
	return table.PGName + "." + col.PGName
}
