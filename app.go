package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type app struct {
	db     *sql.DB
	static fs.FS
}

type event struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	CreatedAt string `json:"created_at"`
}

type occurrence struct {
	EventID   int64  `json:"event_id"`
	Date      string `json:"date"`
	Intensity *int   `json:"intensity,omitempty"`
}

type stats struct {
	EventID          int64   `json:"event_id"`
	Total            int     `json:"total"`
	ThisWeek         int     `json:"this_week"`
	ThisMonth        int     `json:"this_month"`
	AveragePerWeek   float64 `json:"average_per_week"`
	AveragePerMonth  float64 `json:"average_per_month"`
	CurrentStreak    int     `json:"current_streak"`
	LongestStreak    int     `json:"longest_streak"`
	FirstOccurrence  string  `json:"first_occurrence,omitempty"`
	LatestOccurrence string  `json:"latest_occurrence,omitempty"`
}

var (
	datePattern  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL COLLATE NOCASE UNIQUE,
			color TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		);
		CREATE TABLE IF NOT EXISTS occurrences (
			event_id INTEGER NOT NULL REFERENCES events(id) ON DELETE CASCADE,
			date TEXT NOT NULL,
			intensity INTEGER CHECK (intensity BETWEEN 1 AND 10),
			PRIMARY KEY (event_id, date)
		);
		CREATE INDEX IF NOT EXISTS idx_occurrences_date ON occurrences(date);
	`); err != nil {
		db.Close()
		return nil, err
	}
	rows, err := db.Query(`PRAGMA table_info(occurrences)`)
	if err != nil {
		db.Close()
		return nil, err
	}
	hasIntensity := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			db.Close()
			return nil, err
		}
		if name == "intensity" {
			hasIntensity = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		db.Close()
		return nil, err
	}
	rows.Close()
	if !hasIntensity {
		if _, err := db.Exec(`ALTER TABLE occurrences ADD COLUMN intensity INTEGER CHECK (intensity BETWEEN 1 AND 10)`); err != nil {
			db.Close()
			return nil, err
		}
	}
	return db, nil
}

func newApp(db *sql.DB, static fs.FS) *app { return &app{db: db, static: static} }

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", a.health)
	mux.HandleFunc("GET /api/events", a.listEvents)
	mux.HandleFunc("POST /api/events", a.createEvent)
	mux.HandleFunc("PATCH /api/events/{id}", a.updateEvent)
	mux.HandleFunc("DELETE /api/events/{id}", a.deleteEvent)
	mux.HandleFunc("GET /api/occurrences", a.listOccurrences)
	mux.HandleFunc("PUT /api/occurrences", a.addOccurrence)
	mux.HandleFunc("DELETE /api/occurrences", a.removeOccurrence)
	mux.HandleFunc("GET /api/stats/{id}", a.eventStats)
	mux.Handle("/", http.FileServer(http.FS(a.static)))
	return recoverMiddleware(logMiddleware(mux))
}

func (a *app) health(w http.ResponseWriter, _ *http.Request) {
	if err := a.db.Ping(); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *app) listEvents(w http.ResponseWriter, _ *http.Request) {
	rows, err := a.db.Query(`SELECT id, name, color, created_at FROM events ORDER BY name COLLATE NOCASE`)
	if err != nil {
		writeError(w, 500, "could not load events")
		return
	}
	defer rows.Close()
	events := []event{}
	for rows.Next() {
		var item event
		if err := rows.Scan(&item.ID, &item.Name, &item.Color, &item.CreatedAt); err != nil {
			writeError(w, 500, "could not load events")
			return
		}
		events = append(events, item)
	}
	writeJSON(w, http.StatusOK, events)
}

func (a *app) createEvent(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Color == "" {
		input.Color = "#557A67"
	}
	if err := validateEvent(input.Name, input.Color); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.db.Exec(`INSERT INTO events(name, color) VALUES (?, ?)`, input.Name, input.Color)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeError(w, http.StatusConflict, "an event with this name already exists")
			return
		}
		writeError(w, 500, "could not create event")
		return
	}
	id, _ := result.LastInsertId()
	var item event
	if err := a.db.QueryRow(`SELECT id, name, color, created_at FROM events WHERE id = ?`, id).Scan(&item.ID, &item.Name, &item.Color, &item.CreatedAt); err != nil {
		writeError(w, 500, "event created but could not be loaded")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (a *app) updateEvent(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var input struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if err := validateEvent(input.Name, input.Color); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.db.Exec(`UPDATE events SET name = ?, color = ? WHERE id = ?`, input.Name, input.Color, id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeError(w, http.StatusConflict, "an event with this name already exists")
			return
		}
		writeError(w, 500, "could not update event")
		return
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "name": input.Name, "color": input.Color})
}

func (a *app) deleteEvent(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	result, err := a.db.Exec(`DELETE FROM events WHERE id = ?`, id)
	if err != nil {
		writeError(w, 500, "could not delete event")
		return
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) listOccurrences(w http.ResponseWriter, r *http.Request) {
	start, end := r.URL.Query().Get("start"), r.URL.Query().Get("end")
	if !validDate(start) || !validDate(end) || start > end {
		writeError(w, http.StatusBadRequest, "start and end must be valid dates")
		return
	}
	rows, err := a.db.Query(`SELECT event_id, date, intensity FROM occurrences WHERE date BETWEEN ? AND ? ORDER BY date, event_id`, start, end)
	if err != nil {
		writeError(w, 500, "could not load occurrences")
		return
	}
	defer rows.Close()
	items := []occurrence{}
	for rows.Next() {
		var item occurrence
		if err := rows.Scan(&item.EventID, &item.Date, &item.Intensity); err != nil {
			writeError(w, 500, "could not load occurrences")
			return
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *app) addOccurrence(w http.ResponseWriter, r *http.Request) {
	item, ok := decodeOccurrence(w, r)
	if !ok {
		return
	}
	_, err := a.db.Exec(`INSERT INTO occurrences(event_id, date, intensity) VALUES (?, ?, ?)
		ON CONFLICT(event_id, date) DO UPDATE SET intensity = excluded.intensity`, item.EventID, item.Date, item.Intensity)
	if err != nil {
		if strings.Contains(err.Error(), "FOREIGN KEY") {
			writeError(w, http.StatusNotFound, "event not found")
			return
		}
		writeError(w, 500, "could not mark event")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *app) removeOccurrence(w http.ResponseWriter, r *http.Request) {
	item, ok := decodeOccurrence(w, r)
	if !ok {
		return
	}
	_, err := a.db.Exec(`DELETE FROM occurrences WHERE event_id = ? AND date = ?`, item.EventID, item.Date)
	if err != nil {
		writeError(w, 500, "could not unmark event")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) eventStats(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	todayString := r.URL.Query().Get("today")
	if todayString == "" {
		todayString = time.Now().Format("2006-01-02")
	}
	if !validDate(todayString) {
		writeError(w, http.StatusBadRequest, "today must be a valid date")
		return
	}
	today, _ := time.Parse("2006-01-02", todayString)
	var exists int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM events WHERE id = ?`, id).Scan(&exists); err != nil || exists == 0 {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	rows, err := a.db.Query(`SELECT date FROM occurrences WHERE event_id = ? AND date <= ? ORDER BY date`, id, todayString)
	if err != nil {
		writeError(w, 500, "could not calculate statistics")
		return
	}
	defer rows.Close()
	dates := []time.Time{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			writeError(w, 500, "could not calculate statistics")
			return
		}
		parsed, _ := time.Parse("2006-01-02", raw)
		dates = append(dates, parsed)
	}
	result := calculateStats(id, dates, today)
	writeJSON(w, http.StatusOK, result)
}

func calculateStats(eventID int64, dates []time.Time, today time.Time) stats {
	result := stats{EventID: eventID, Total: len(dates)}
	weekStart := today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
	monthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	seen := make(map[string]bool, len(dates))
	for _, date := range dates {
		seen[date.Format("2006-01-02")] = true
		if !date.Before(weekStart) {
			result.ThisWeek++
		}
		if !date.Before(monthStart) {
			result.ThisMonth++
		}
	}
	if len(dates) == 0 {
		return result
	}
	result.FirstOccurrence = dates[0].Format("2006-01-02")
	result.LatestOccurrence = dates[len(dates)-1].Format("2006-01-02")
	daysObserved := int(today.Sub(dates[0]).Hours()/24) + 1
	result.AveragePerWeek = float64(len(dates)) / (float64(daysObserved) / 7)
	result.AveragePerMonth = float64(len(dates)) / (float64(daysObserved) / (365.2425 / 12))
	if daysObserved < 7 {
		result.AveragePerWeek = float64(len(dates))
	}
	if daysObserved < 30 {
		result.AveragePerMonth = float64(len(dates))
	}

	streak := 0
	previous := time.Time{}
	for _, date := range dates {
		if previous.IsZero() || date.Sub(previous) == 24*time.Hour {
			streak++
		} else {
			streak = 1
		}
		if streak > result.LongestStreak {
			result.LongestStreak = streak
		}
		previous = date
	}
	for cursor := today; seen[cursor.Format("2006-01-02")]; cursor = cursor.AddDate(0, 0, -1) {
		result.CurrentStreak++
	}
	return result
}

func decodeOccurrence(w http.ResponseWriter, r *http.Request) (occurrence, bool) {
	var item occurrence
	if !decodeJSON(w, r, &item) {
		return item, false
	}
	if item.EventID < 1 || !validDate(item.Date) {
		writeError(w, http.StatusBadRequest, "event_id and a valid date are required")
		return item, false
	}
	if item.Intensity != nil && (*item.Intensity < 1 || *item.Intensity > 10) {
		writeError(w, http.StatusBadRequest, "intensity must be between 1 and 10")
		return item, false
	}
	return item, true
}

func validateEvent(name, color string) error {
	if name == "" || len([]rune(name)) > 60 {
		return errors.New("name must be between 1 and 60 characters")
	}
	if !colorPattern.MatchString(color) {
		return errors.New("color must be a six-digit hex value")
	}
	return nil
}

func validDate(value string) bool {
	if !datePattern.MatchString(value) {
		return false
	}
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}

func parseID(w http.ResponseWriter, value string) (int64, bool) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid event id")
		return 0, false
	}
	return id, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(started).Round(time.Millisecond))
	})
}

func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("panic: %v", recovered)
				writeError(w, 500, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s stats) String() string { return fmt.Sprintf("event %d: %d total", s.EventID, s.Total) }
