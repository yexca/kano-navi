package sqlite

import "testing"

// shop is the synthetic shop page every announcement of one sale links.
const shop = "url='https://shop.example.test/collections/synthetic/'"

func TestReminderOfASaleMergesIntoTheAnnouncedPeriod(t *testing.T) {
	f := newMergeFixture(t)
	// The announcement: 10/20 17:00 to 11/3 23:59.
	f.add("x-period", "x", "コラボグッズ受付", "2026-10-20", "17:00", "sale", shop,
		"ends_on='2026-11-03'", "ends_at='2026-11-03T14:59:00.000Z'")
	// Two reminders of the same deadline, one linking the shop with a www
	// host and no trailing slash, one linking nothing but naming the goods.
	f.add("x-left", "x", "残り2日", "2026-11-03", "", "sale",
		"url='https://www.shop.example.test/collections/synthetic'", "ends_on='2026-11-03'", "ends_at='2026-11-03T14:59:00.000Z'")
	f.add("x-today", "x", "コラボグッズ受付 本日まで", "2026-11-03", "", "sale", "ends_on='2026-11-03'")
	// Another shop's sale in the same period stays.
	f.add("x-other", "x", "受注期間", "2026-11-03", "", "sale",
		"url='https://other.example.test/goods'", "ends_on='2026-11-03'")
	counts := f.reconcile()
	for id, want := range map[string]string{
		"x-left":   "x-period|rule|rule_c:sale_period",
		"x-today":  "x-period|rule|rule_c:sale_period",
		"x-other":  "||",
		"x-period": "||",
	} {
		if got := f.state(id); got != want {
			t.Errorf("%s = %q, want %q", id, got, want)
		}
	}
	if counts.Merged != 2 || counts.Considered != 4 {
		t.Fatalf("counts = %+v", counts)
	}
	// The decision is stable.
	if counts = f.reconcile(); counts.Merged+counts.Retargeted+counts.Released != 0 || counts.Kept != 2 {
		t.Fatalf("second run = %+v", counts)
	}
}

func TestASaleWithoutAShopLinkMatchesOnlyByWhatItSells(t *testing.T) {
	f := newMergeFixture(t)
	f.add("x-period", "x", "鹿乃まほろ コラボグッズ受注期間", "2026-10-20", "", "sale", shop, "ends_on='2026-11-03'")
	// Entries that link the post rather than a shop: one names the goods,
	// the others only words and counts every sale uses, or Kano's name,
	// which names no goods.
	f.add("x-goods", "x", "コラボグッズ 残り2日", "2026-11-03", "", "sale",
		"url='https://x.com/synthetic_handle/status/1'", "ends_on='2026-11-03'")
	f.add("x-generic", "x", "受注 本日まで", "2026-11-03", "", "sale",
		"url='https://x.com/synthetic_handle/status/2'", "ends_on='2026-11-03'")
	f.add("x-count", "x", "受付終了まで残り2日", "2026-11-03", "", "sale", "ends_on='2026-11-03'")
	f.add("x-named", "x", "鹿乃まほろ 誕生日ボイス 販売", "2026-10-25", "", "sale",
		"url='https://x.com/synthetic_handle/status/3'", "ends_on='2026-11-01'")
	f.reconcile()
	for id, want := range map[string]string{
		"x-goods":   "x-period|rule|rule_c:sale_period",
		"x-generic": "||",
		"x-count":   "||",
		"x-named":   "||",
		"x-period":  "||",
	} {
		if got := f.state(id); got != want {
			t.Errorf("%s = %q, want %q", id, got, want)
		}
	}
}

func TestASaleMergesOnlyIntoAPeriodThatCoversIt(t *testing.T) {
	f := newMergeFixture(t)
	f.add("x-period", "x", "受注", "2026-10-20", "", "sale", shop, "ends_on='2026-11-03'")
	// A deadline on another date, and a period that runs longer, are other
	// windows of the same shop; the deadline does not fall at either close.
	f.add("x-later-deadline", "x", "受注", "2026-11-10", "", "sale", shop, "ends_on='2026-11-10'")
	f.add("x-longer", "x", "受注", "2026-10-25", "", "sale", shop, "ends_on='2026-11-20'")
	// The opening alone, inside the period, is covered.
	f.add("x-opening", "x", "受注開始", "2026-10-20", "12:00", "sale", shop)
	f.reconcile()
	for id, want := range map[string]string{
		"x-later-deadline": "||",
		"x-longer":         "||",
		"x-opening":        "x-period|rule|rule_c:sale_period",
		"x-period":         "||",
	} {
		if got := f.state(id); got != want {
			t.Errorf("%s = %q, want %q", id, got, want)
		}
	}
}

func TestTheSamePeriodKeepsTheEntryThatStatesMore(t *testing.T) {
	f := newMergeFixture(t)
	f.add("x-b-dates", "x", "受注", "2026-10-20", "", "sale", shop, "ends_on='2026-11-03'")
	f.add("x-c-times", "x", "受注", "2026-10-20", "12:00", "sale", shop,
		"ends_on='2026-11-03'", "ends_at='2026-11-03T14:59:00.000Z'")
	f.add("x-a-dates", "x", "受注", "2026-10-20", "", "sale", shop, "ends_on='2026-11-03'")
	// An operator's entry is never merged away but receives the others.
	f.add("x-d-manual", "x", "受注", "2026-10-20", "", "sale", shop, "ends_on='2026-11-03'", "provenance='manual'", "manual_locked=1")
	f.reconcile()
	for id, want := range map[string]string{
		"x-a-dates":  "x-c-times|rule|rule_c:sale_period",
		"x-b-dates":  "x-c-times|rule|rule_c:sale_period",
		"x-c-times":  "||",
		"x-d-manual": "||",
	} {
		if got := f.state(id); got != want {
			t.Errorf("%s = %q, want %q", id, got, want)
		}
	}
}

func TestTwoPeriodsThatDoNotCoverEachOtherLeaveASaleUnmerged(t *testing.T) {
	f := newMergeFixture(t)
	f.add("x-first", "x", "受注", "2026-10-20", "", "sale", shop, "ends_on='2026-11-03'")
	f.add("x-second", "x", "受注", "2026-10-25", "", "sale", shop, "ends_on='2026-11-05'")
	f.add("x-deadline", "x", "受注", "2026-10-28", "", "sale", shop, "ends_on='2026-10-28'")
	// A deadline is covered only by a period closing on its date, so the
	// opening-day entry inside both periods is the ambiguous one.
	f.add("x-opening", "x", "受注開始", "2026-10-26", "", "sale", shop)
	counts := f.reconcile()
	for _, id := range []string{"x-first", "x-second", "x-deadline", "x-opening"} {
		if got := f.state(id); got != "||" {
			t.Errorf("%s = %q", id, got)
		}
	}
	if counts.Ambiguous != 1 {
		t.Fatalf("counts = %+v", counts)
	}
}

func TestASalePeriodOpenedBeforeTheWindowStillReceivesItsReminder(t *testing.T) {
	f := newMergeFixture(t)
	// Opened 60 days before mergeNow, closes inside the window.
	f.add("x-period", "x", "受注", "2026-08-25", "", "sale", shop, "ends_on='2026-10-30'")
	f.add("x-deadline", "x", "受注", "2026-10-30", "", "sale", shop, "ends_on='2026-10-30'")
	f.reconcile()
	if got := f.state("x-deadline"); got != "x-period|rule|rule_c:sale_period" {
		t.Fatalf("x-deadline = %q", got)
	}
}
