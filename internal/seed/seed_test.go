package seed

import (
	"testing"

	"github.com/jwald3/tandem/internal/store"
)

func TestDemo(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	if err := Demo(st); err != nil {
		t.Fatal(err)
	}
	cs, _ := st.ListContacts("")
	if len(cs) != 5 {
		t.Fatalf("want 5 contacts, got %d", len(cs))
	}
	open, _ := st.ListReminders(store.ReminderFilter{})
	if len(open) != 7 {
		t.Fatalf("want 7 open reminders, got %d", len(open))
	}
	if err := Demo(st); err != ErrNotEmpty {
		t.Fatalf("seeding twice should refuse: %v", err)
	}
}
