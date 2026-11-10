package sqlite

import "fmt"

// eventEvidenceSQL lists, as the alias es, the active event_sources rows of
// the event named by the table reference table and of the events merged into
// it: a notice merged into an event counts as that event's evidence.
func eventEvidenceSQL(table, es string) string {
	return fmt.Sprintf(`event_sources %[2]s WHERE %[2]s.deleted_at IS NULL AND (%[2]s.event_id=%[1]s.id OR %[2]s.event_id IN (SELECT %[2]sm.id FROM events %[2]sm WHERE %[2]sm.merged_into=%[1]s.id AND %[2]sm.deleted_at IS NULL))`, table, es)
}

// hiddenPostEvidenceSQL holds for the evidence row es when it is a stored X
// or Bilibili post that an operator hid. The unary plus keeps the source test
// off the identity index, so the planner finds the event's rows through
// event_sources_event_idx instead of walking every X and Bilibili row for
// each event.
func hiddenPostEvidenceSQL(es string) string {
	return fmt.Sprintf(`+%[1]s.source IN ('x','bilibili') AND EXISTS (SELECT 1 FROM posts %[1]sp WHERE %[1]sp.id=%[1]s.source_item_id AND %[1]sp.source=%[1]s.source AND %[1]sp.hidden_at IS NOT NULL)`, es)
}

// eventHiddenByPostsSQL holds for an automatic, unlocked event whose every
// active source, its own and those of the events merged into it, is a hidden
// post. It is a visibility rule only: the row, its evidence and its merges
// stay, and showing a source post shows the event again. An event an
// operator edited, confirmed or added is never hidden this way, and neither
// is an event with any other source, such as a YouTube reservation.
// table is the events table reference of the enclosing query.
func eventHiddenByPostsSQL(table string) string {
	return fmt.Sprintf(`(%[1]s.provenance<>'manual' AND %[1]s.manual_locked=0 AND EXISTS (SELECT 1 FROM %[2]s AND %[3]s) AND NOT EXISTS (SELECT 1 FROM %[4]s AND NOT (%[5]s)))`,
		table, eventEvidenceSQL(table, "hes"), hiddenPostEvidenceSQL("hes"), eventEvidenceSQL(table, "ves"), hiddenPostEvidenceSQL("ves"))
}

// eventShownSQL holds for an event that is not hidden by its posts. Every
// public event read applies it next to the tombstone and merge exclusions.
func eventShownSQL(table string) string {
	return "NOT " + eventHiddenByPostsSQL(table)
}
