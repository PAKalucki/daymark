package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"testing/fstest"
	"time"
)

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	static := fstest.MapFS{"index.html": {Data: []byte("ok")}}
	server := httptest.NewServer(newApp(db, static).routes())
	t.Cleanup(server.Close)
	return server
}

func request(t *testing.T, client *http.Client, method, url string, body any) (*http.Response, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return response, payload
}

func createTestEvent(t *testing.T, server *httptest.Server, name string) event {
	t.Helper()
	response, payload := request(t, server.Client(), http.MethodPost, server.URL+"/api/events", map[string]any{
		"name": name, "color": "#557A67",
	})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create event: status %d: %s", response.StatusCode, payload)
	}
	var created event
	if err := json.Unmarshal(payload, &created); err != nil {
		t.Fatal(err)
	}
	return created
}

func TestEventAndOccurrenceLifecycle(t *testing.T) {
	server := testServer(t)
	created := createTestEvent(t, server, "Morning run")

	response, payload := request(t, server.Client(), http.MethodPost, server.URL+"/api/events", map[string]any{
		"name": "morning RUN", "color": "#C66B4E",
	})
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate event: got %d, want 409: %s", response.StatusCode, payload)
	}

	mark := map[string]any{"event_id": created.ID, "date": "2026-09-17"}
	for range 2 {
		response, payload = request(t, server.Client(), http.MethodPut, server.URL+"/api/occurrences", mark)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("mark event: status %d: %s", response.StatusCode, payload)
		}
	}

	response, payload = request(t, server.Client(), http.MethodGet, server.URL+"/api/occurrences?start=2026-09-01&end=2026-09-30", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("list occurrences: status %d: %s", response.StatusCode, payload)
	}
	var occurrences []occurrence
	if err := json.Unmarshal(payload, &occurrences); err != nil {
		t.Fatal(err)
	}
	if len(occurrences) != 1 {
		t.Fatalf("got %d occurrences, want 1", len(occurrences))
	}

	response, payload = request(t, server.Client(), http.MethodDelete, server.URL+"/api/events/"+strconv.FormatInt(created.ID, 10), nil)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("delete event: status %d: %s", response.StatusCode, payload)
	}
	_, payload = request(t, server.Client(), http.MethodGet, server.URL+"/api/occurrences?start=2026-09-01&end=2026-09-30", nil)
	if string(payload) != "[]\n" {
		t.Fatalf("occurrences were not deleted with event: %s", payload)
	}
}

func TestStatistics(t *testing.T) {
	dates := []time.Time{
		time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC),
	}
	got := calculateStats(7, dates, time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC))
	if got.Total != 5 || got.ThisWeek != 4 || got.ThisMonth != 4 {
		t.Fatalf("unexpected counts: %+v", got)
	}
	if got.CurrentStreak != 4 || got.LongestStreak != 4 {
		t.Fatalf("unexpected streaks: %+v", got)
	}
	if got.FirstOccurrence != "2026-08-31" || got.LatestOccurrence != "2026-09-17" {
		t.Fatalf("unexpected range: %+v", got)
	}
}

func TestRejectsInvalidDates(t *testing.T) {
	server := testServer(t)
	created := createTestEvent(t, server, "Read")
	response, _ := request(t, server.Client(), http.MethodPut, server.URL+"/api/occurrences", map[string]any{
		"event_id": created.ID, "date": "2026-02-30",
	})
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", response.StatusCode)
	}
}

func TestOccurrenceIntensity(t *testing.T) {
	server := testServer(t)
	event := createTestEvent(t, server, "Headache")
	other := createTestEvent(t, server, "Exercise")
	url := server.URL + "/api/occurrences"

	for _, input := range []map[string]any{
		{"event_id": event.ID, "date": "2026-09-17", "intensity": 1},
		{"event_id": event.ID, "date": "2026-09-18", "intensity": 10},
		{"event_id": other.ID, "date": "2026-09-17"},
	} {
		response, payload := request(t, server.Client(), http.MethodPut, url, input)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("mark occurrence: status %d: %s", response.StatusCode, payload)
		}
	}

	for _, intensity := range []any{0, 11, -1, 1.5, "5"} {
		response, _ := request(t, server.Client(), http.MethodPut, url, map[string]any{
			"event_id": event.ID, "date": "2026-09-17", "intensity": intensity,
		})
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("intensity %v: status %d, want 400", intensity, response.StatusCode)
		}
	}

	response, payload := request(t, server.Client(), http.MethodPut, url, map[string]any{
		"event_id": event.ID, "date": "2026-09-17", "intensity": 7,
	})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("update intensity: status %d: %s", response.StatusCode, payload)
	}
	response, payload = request(t, server.Client(), http.MethodGet, url+"?start=2026-09-17&end=2026-09-18", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("list occurrences: status %d: %s", response.StatusCode, payload)
	}
	var items []occurrence
	if err := json.Unmarshal(payload, &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[0].Intensity == nil || *items[0].Intensity != 7 || items[1].Intensity != nil || items[2].Intensity == nil || *items[2].Intensity != 10 {
		t.Fatalf("intensities are not independent: %+v", items)
	}

	response, payload = request(t, server.Client(), http.MethodPut, url, map[string]any{
		"event_id": event.ID, "date": "2026-09-17", "intensity": nil,
	})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("clear intensity: status %d: %s", response.StatusCode, payload)
	}
	if bytes.Contains(payload, []byte(`"intensity"`)) {
		t.Fatalf("cleared intensity should be omitted: %s", payload)
	}
	_, payload = request(t, server.Client(), http.MethodGet, url+"?start=2026-09-17&end=2026-09-17", nil)
	items = nil
	if err := json.Unmarshal(payload, &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Intensity != nil || items[1].Intensity != nil {
		t.Fatalf("clearing intensity changed other occurrences: %+v", items)
	}
}

func TestOpenDBMigratesExistingOccurrences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.db")
	oldDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = oldDB.Exec(`
		CREATE TABLE events (id INTEGER PRIMARY KEY, name TEXT NOT NULL, color TEXT NOT NULL, created_at TEXT NOT NULL);
		CREATE TABLE occurrences (event_id INTEGER NOT NULL, date TEXT NOT NULL, PRIMARY KEY (event_id, date));
		INSERT INTO events VALUES (1, 'Headache', '#557A67', '2026-09-17T00:00:00Z');
		INSERT INTO occurrences VALUES (1, '2026-09-17');
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := oldDB.Close(); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		db, err := openDB(path)
		if err != nil {
			t.Fatal(err)
		}
		var intensity sql.NullInt64
		if err := db.QueryRow(`SELECT intensity FROM occurrences WHERE event_id = 1 AND date = '2026-09-17'`).Scan(&intensity); err != nil {
			t.Fatal(err)
		}
		if intensity.Valid {
			t.Fatalf("migrated occurrence intensity should be unset: %v", intensity.Int64)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
