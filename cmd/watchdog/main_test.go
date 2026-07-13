package main

import (
	"os"
	"testing"
)

// resetNotifyGlobals rétablit une config de notification connue pour les tests.
func resetNotifyGlobals(t *testing.T) {
	t.Helper()
	old1, old2, old3 := notifyEnabled, notifyPercent, limitBytes
	notifyEnabled = true
	notifyPercent = 80
	limitBytes = 1000
	t.Cleanup(func() {
		notifyEnabled, notifyPercent, limitBytes = old1, old2, old3
	})
}

// rowsWith construit des lignes de process avec le seuil courant (limitBytes).
func rowsWith(pairs ...[2]int) []procRow {
	var rows []procRow
	for _, p := range pairs {
		rows = append(rows, procRow{pid: p[0], rss: uint64(p[1]), limit: limitBytes})
	}
	return rows
}

func TestCheckNotificationsTransition(t *testing.T) {
	resetNotifyGlobals(t)
	m := model{notified: map[int]bool{}}

	// Franchissement initial : pid 1 à 85 %, pid 2 à 50 %.
	m.rows = rowsWith([2]int{1, 850}, [2]int{2, 500})
	msgs := m.checkNotifications()
	if len(msgs) != 1 {
		t.Fatalf("attendu 1 notification, obtenu %d : %v", len(msgs), msgs)
	}
	if want := "85%"; !contains(msgs[0], want) {
		t.Errorf("message %q ne contient pas %q", msgs[0], want)
	}
	if !m.notified[1] || m.notified[2] {
		t.Errorf("état notified inattendu : %v", m.notified)
	}

	// Deuxième scan identique : pas de nouvelle notification (anti-spam).
	if msgs := m.checkNotifications(); len(msgs) != 0 {
		t.Errorf("aucune notification attendue au re-scan, obtenu %v", msgs)
	}

	// pid 1 repasse sous le seuil : il est oublié.
	m.rows = rowsWith([2]int{1, 500})
	if msgs := m.checkNotifications(); len(msgs) != 0 {
		t.Errorf("aucune notification sous le seuil, obtenu %v", msgs)
	}
	if m.notified[1] {
		t.Errorf("pid 1 aurait dû être oublié")
	}

	// pid 1 re-franchit le seuil : re-notification.
	m.rows = rowsWith([2]int{1, 900})
	if msgs := m.checkNotifications(); len(msgs) != 1 {
		t.Errorf("re-notification attendue au re-franchissement, obtenu %v", msgs)
	}
}

func TestCheckNotificationsDisabled(t *testing.T) {
	resetNotifyGlobals(t)
	notifyEnabled = false
	m := model{notified: map[int]bool{}, rows: rowsWith([2]int{1, 999})}
	if msgs := m.checkNotifications(); msgs != nil {
		t.Errorf("notifications désactivées : attendu nil, obtenu %v", msgs)
	}
}

func TestCheckNotificationsCustomPercent(t *testing.T) {
	resetNotifyGlobals(t)
	notifyPercent = 50 // seuil abaissé à 50 %
	m := model{notified: map[int]bool{}, rows: rowsWith([2]int{1, 600}, [2]int{2, 400})}
	msgs := m.checkNotifications()
	if len(msgs) != 1 { // pid 1 à 60 % franchit, pid 2 à 40 % non
		t.Fatalf("attendu 1 notification à 50%%, obtenu %d : %v", len(msgs), msgs)
	}
}

func TestLoadConfigPerTargetLimits(t *testing.T) {
	// Sauvegarde et restauration des globals touchés par loadConfig.
	op, ol, opp := patterns, limitBytes, perPatternLimit
	t.Cleanup(func() { patterns, limitBytes, perPatternLimit = op, ol, opp })

	cfg := `{
		"limit": "3GiB",
		"patterns": ["webex"],
		"targets": [
			{"pattern": "firefox", "limit": "2GiB"},
			{"pattern": "chrome"}
		]
	}`
	path := t.TempDir() + "/c.json"
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := loadConfig(path, true); err != nil {
		t.Fatalf("loadConfig : %v", err)
	}

	if limitOf("webex") != 3<<30 {
		t.Errorf("webex : seuil par défaut attendu 3GiB, obtenu %d", limitOf("webex"))
	}
	if limitOf("firefox") != 2<<30 {
		t.Errorf("firefox : seuil propre attendu 2GiB, obtenu %d", limitOf("firefox"))
	}
	if limitOf("chrome") != 3<<30 { // pas de limit -> repli sur le défaut
		t.Errorf("chrome : repli défaut 3GiB attendu, obtenu %d", limitOf("chrome"))
	}
	want := []string{"webex", "firefox", "chrome"}
	if len(patterns) != len(want) {
		t.Fatalf("patterns attendus %v, obtenu %v", want, patterns)
	}
	for i := range want {
		if patterns[i] != want[i] {
			t.Errorf("patterns[%d] = %q, attendu %q", i, patterns[i], want[i])
		}
	}
}

func TestLoadConfigEmptyTargetPattern(t *testing.T) {
	op, ol, opp := patterns, limitBytes, perPatternLimit
	t.Cleanup(func() { patterns, limitBytes, perPatternLimit = op, ol, opp })

	path := t.TempDir() + "/c.json"
	os.WriteFile(path, []byte(`{"targets":[{"limit":"1GiB"}]}`), 0o644)
	if err := loadConfig(path, true); err == nil {
		t.Error("un pattern vide dans targets devrait provoquer une erreur")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
