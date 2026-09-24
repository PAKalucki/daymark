const colors = ["#557A67", "#C66B4E", "#D3A53D", "#6D79A8", "#9A657E", "#4D8991", "#7E8B55", "#B65D65"];
const state = {
  month: new Date(new Date().getFullYear(), new Date().getMonth(), 1),
  events: [],
  occurrences: [],
  selectedEventId: null,
  selectedDate: null,
};

const els = {
  monthTitle: document.querySelector("#month-title"),
  calendar: document.querySelector("#calendar-grid"),
  eventList: document.querySelector("#event-list"),
  stats: document.querySelector("#stats-content"),
  eventDialog: document.querySelector("#event-dialog"),
  eventForm: document.querySelector("#event-form"),
  eventId: document.querySelector("#event-id"),
  eventName: document.querySelector("#event-name"),
  eventError: document.querySelector("#event-form-error"),
  eventTitle: document.querySelector("#event-dialog-title"),
  eventKicker: document.querySelector("#event-dialog-kicker"),
  saveEvent: document.querySelector("#save-event"),
  deleteEvent: document.querySelector("#delete-event"),
  colorOptions: document.querySelector("#color-options"),
  dayDialog: document.querySelector("#day-dialog"),
  dayTitle: document.querySelector("#day-dialog-title"),
  dayEventList: document.querySelector("#day-event-list"),
  toast: document.querySelector("#toast"),
};

function localDate(date) {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

function calendarRange() {
  const first = new Date(state.month);
  const mondayOffset = (first.getDay() + 6) % 7;
  const start = new Date(first);
  start.setDate(first.getDate() - mondayOffset);
  const end = new Date(start);
  end.setDate(start.getDate() + 41);
  return { start, end };
}

async function api(path, options = {}) {
  const headers = options.body ? { "Content-Type": "application/json", ...options.headers } : options.headers;
  const response = await fetch(path, { ...options, headers });
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body.error || `Request failed (${response.status})`);
  }
  if (response.status === 204) return null;
  return response.json();
}

async function loadEvents() {
  state.events = await api("/api/events");
  if (state.selectedEventId && !state.events.some((event) => event.id === state.selectedEventId)) {
    state.selectedEventId = null;
  }
  if (!state.selectedEventId && state.events.length) state.selectedEventId = state.events[0].id;
  renderEvents();
  await renderStats();
}

async function loadCalendar() {
  const { start, end } = calendarRange();
  state.occurrences = await api(`/api/occurrences?start=${localDate(start)}&end=${localDate(end)}`);
  renderCalendar();
}

function renderEvents() {
  els.eventList.replaceChildren();
  if (!state.events.length) {
    const empty = document.createElement("p");
    empty.className = "empty-events";
    empty.textContent = "Create your first event, then mark it on any day.";
    els.eventList.append(empty);
    return;
  }
  state.events.forEach((event) => {
    const row = document.createElement("button");
    row.type = "button";
    row.className = `event-row${event.id === state.selectedEventId ? " selected" : ""}`;
    row.dataset.eventId = event.id;
    row.setAttribute("aria-pressed", String(event.id === state.selectedEventId));
    row.innerHTML = `<span class="event-dot" style="background:${event.color}"></span><span class="event-name"></span><span class="edit-event" role="button" aria-label="Edit ${escapeHTML(event.name)}" title="Edit event">···</span>`;
    row.querySelector(".event-name").textContent = event.name;
    row.addEventListener("click", (clickEvent) => {
      if (clickEvent.target.closest(".edit-event")) {
        openEventDialog(event);
        return;
      }
      state.selectedEventId = event.id;
      renderEvents();
      renderStats();
    });
    els.eventList.append(row);
  });
}

function renderCalendar() {
  els.monthTitle.textContent = state.month.toLocaleDateString(undefined, { month: "long", year: "numeric" });
  els.calendar.replaceChildren();
  const { start } = calendarRange();
  const today = localDate(new Date());
  const byDate = new Map();
  state.occurrences.forEach((item) => {
    const existing = byDate.get(item.date) || [];
    const event = state.events.find((candidate) => candidate.id === item.event_id);
    if (event) existing.push({ event, intensity: item.intensity });
    byDate.set(item.date, existing);
  });

  for (let index = 0; index < 42; index += 1) {
    const date = new Date(start);
    date.setDate(start.getDate() + index);
    const dateString = localDate(date);
    const marks = byDate.get(dateString) || [];
    const button = document.createElement("button");
    button.type = "button";
    button.className = `day${date.getMonth() !== state.month.getMonth() ? " outside" : ""}${dateString === today ? " today" : ""}`;
    const markSummary = marks.map(({ event, intensity }) => `${event.name}${intensity == null ? "" : `, intensity ${intensity} out of 10`}`).join("; ");
    button.setAttribute("aria-label", `${date.toLocaleDateString(undefined, { weekday: "long", month: "long", day: "numeric" })}${markSummary ? `, ${markSummary}` : ""}`);
    const number = document.createElement("span");
    number.className = "day-number";
    number.textContent = date.getDate();
    const marksNode = document.createElement("span");
    marksNode.className = "marks";
    marks.slice(0, 3).forEach(({ event, intensity }) => {
      const mark = document.createElement("span");
      mark.className = "mark";
      mark.style.setProperty("--event-color", event.color);
      const name = document.createElement("span");
      name.className = "mark-name";
      name.textContent = event.name;
      mark.append(name);
      if (intensity != null) {
        const value = document.createElement("span");
        value.className = "mark-intensity";
        value.textContent = intensity;
        mark.title = `${event.name}: intensity ${intensity}/10`;
        mark.append(value);
      }
      marksNode.append(mark);
    });
    if (marks.length > 3) {
      const more = document.createElement("span");
      more.className = "more-marks";
      more.textContent = `+${marks.length - 3} more`;
      marksNode.append(more);
    }
    button.append(number, marksNode);
    button.addEventListener("click", () => openDayDialog(dateString));
    els.calendar.append(button);
  }
}

async function renderStats() {
  const event = state.events.find((candidate) => candidate.id === state.selectedEventId);
  if (!event) {
    els.stats.innerHTML = `<div class="stats-placeholder"><span class="stats-placeholder-icon" aria-hidden="true"><i></i><i></i><i></i></span><h3>No event selected</h3><p>Create or select an event to see its patterns.</p></div>`;
    return;
  }
  els.stats.innerHTML = `<div class="stats-placeholder"><p>Calculating…</p></div>`;
  try {
    const data = await api(`/api/stats/${event.id}?today=${localDate(new Date())}`);
    els.stats.innerHTML = `
      <p class="eyebrow">At a glance</p>
      <div class="stats-event-heading"><span class="event-dot" style="background:${event.color}"></span><h2></h2></div>
      <div class="stats-grid">
        <div class="stat-card"><span class="stat-value">${data.this_week}</span><span class="stat-label">This week</span></div>
        <div class="stat-card"><span class="stat-value">${data.this_month}</span><span class="stat-label">This month</span></div>
        <div class="stat-card"><span class="stat-value">${formatAverage(data.average_per_week)}</span><span class="stat-label">Avg. / week</span></div>
        <div class="stat-card"><span class="stat-value">${formatAverage(data.average_per_month)}</span><span class="stat-label">Avg. / month</span></div>
        <div class="stat-card"><span class="stat-value">${data.current_streak}</span><span class="stat-label">Current streak</span></div>
        <div class="stat-card"><span class="stat-value">${data.longest_streak}</span><span class="stat-label">Longest streak</span></div>
        <div class="stat-card wide"><span class="stat-value">${data.total}</span><span class="stat-label">All-time marks</span></div>
      </div>
      <p class="stat-note">${data.first_occurrence ? `Tracking since ${friendlyDate(data.first_occurrence)}. A streak counts consecutive marked days.` : "Mark this event on the calendar to begin seeing its pattern."}</p>`;
    els.stats.querySelector("h2").textContent = event.name;
  } catch (error) {
    els.stats.innerHTML = `<div class="stats-placeholder"><h3>Statistics unavailable</h3><p>${escapeHTML(error.message)}</p></div>`;
  }
}

function openEventDialog(event = null) {
  els.eventForm.reset();
  els.eventError.textContent = "";
  els.eventId.value = event?.id || "";
  els.eventName.value = event?.name || "";
  els.eventTitle.textContent = event ? "Edit event" : "Create event";
  els.eventKicker.textContent = event ? "Marker settings" : "New marker";
  els.saveEvent.textContent = event ? "Save changes" : "Create event";
  els.deleteEvent.classList.toggle("hidden", !event);
  const chosenColor = event?.color || colors[state.events.length % colors.length];
  const input = els.colorOptions.querySelector(`input[value="${chosenColor}"]`) || els.colorOptions.querySelector("input");
  if (input) input.checked = true;
  els.eventDialog.showModal();
  requestAnimationFrame(() => els.eventName.focus());
}

function openDayDialog(date) {
  state.selectedDate = date;
  els.dayTitle.textContent = friendlyDate(date, { weekday: "long", month: "long", day: "numeric" });
  renderDayOptions();
  els.dayDialog.showModal();
}

function renderDayOptions() {
  els.dayEventList.replaceChildren();
  if (!state.events.length) {
    const empty = document.createElement("div");
    empty.className = "day-empty";
    empty.textContent = "Create an event first, then return to mark this day.";
    els.dayEventList.append(empty);
    return;
  }
  state.events.forEach((event) => {
    const occurrence = state.occurrences.find((item) => item.event_id === event.id && item.date === state.selectedDate);
    const row = document.createElement("div");
    row.className = "day-event-option";
    row.style.setProperty("--event-color", event.color);
    row.innerHTML = `<label class="day-event-toggle"><input type="checkbox"><span class="event-name"></span><span class="event-dot" style="background:${event.color}"></span></label><label class="intensity-field"><span>Intensity (optional)</span><select aria-label="${escapeHTML(event.name)} intensity"><option value="">Not set</option></select></label>`;
    row.querySelector(".event-name").textContent = event.name;
    const checkbox = row.querySelector("input");
    const intensityField = row.querySelector(".intensity-field");
    const select = row.querySelector("select");
    checkbox.checked = Boolean(occurrence);
    for (let value = 1; value <= 10; value += 1) {
      const option = document.createElement("option");
      option.value = value;
      option.textContent = value;
      select.append(option);
    }
    select.value = occurrence?.intensity == null ? "" : String(occurrence.intensity);
    select.disabled = !occurrence;
    intensityField.classList.toggle("hidden", !occurrence);
    checkbox.addEventListener("change", async (changeEvent) => {
      const checkbox = changeEvent.currentTarget;
      checkbox.disabled = true;
      select.disabled = true;
      try {
        const saved = await api("/api/occurrences", {
          method: checkbox.checked ? "PUT" : "DELETE",
          body: JSON.stringify({ event_id: event.id, date: state.selectedDate }),
        });
        if (checkbox.checked) {
          state.occurrences.push(saved);
        } else {
          state.occurrences = state.occurrences.filter((item) => !(item.event_id === event.id && item.date === state.selectedDate));
          select.value = "";
        }
        select.disabled = !checkbox.checked;
        intensityField.classList.toggle("hidden", !checkbox.checked);
        renderCalendar();
        if (state.selectedEventId === event.id) renderStats();
      } catch (error) {
        checkbox.checked = !checkbox.checked;
        select.disabled = !checkbox.checked;
        showToast(error.message);
      } finally {
        checkbox.disabled = false;
      }
    });
    select.addEventListener("change", async () => {
      const previous = state.occurrences.find((item) => item.event_id === event.id && item.date === state.selectedDate);
      select.disabled = true;
      checkbox.disabled = true;
      try {
        const saved = await api("/api/occurrences", {
          method: "PUT",
          body: JSON.stringify({ event_id: event.id, date: state.selectedDate, intensity: select.value === "" ? null : Number(select.value) }),
        });
        state.occurrences = state.occurrences.map((item) => item.event_id === event.id && item.date === state.selectedDate ? saved : item);
        renderCalendar();
      } catch (error) {
        select.value = previous?.intensity == null ? "" : String(previous.intensity);
        showToast(error.message);
      } finally {
        select.disabled = false;
        checkbox.disabled = false;
      }
    });
    els.dayEventList.append(row);
  });
}

function buildColorOptions() {
  colors.forEach((color, index) => {
    const label = document.createElement("label");
    label.className = "color-option";
    label.style.setProperty("--choice", color);
    label.innerHTML = `<input type="radio" name="color" value="${color}" ${index === 0 ? "checked" : ""} aria-label="Color ${index + 1}"><span></span>`;
    els.colorOptions.append(label);
  });
}

els.eventForm.addEventListener("submit", async (submitEvent) => {
  submitEvent.preventDefault();
  const id = Number(els.eventId.value) || null;
  const body = {
    name: els.eventName.value.trim(),
    color: new FormData(els.eventForm).get("color"),
  };
  els.saveEvent.disabled = true;
  els.eventError.textContent = "";
  try {
    const saved = await api(id ? `/api/events/${id}` : "/api/events", {
      method: id ? "PATCH" : "POST",
      body: JSON.stringify(body),
    });
    if (!id) state.selectedEventId = saved.id;
    els.eventDialog.close();
    await loadEvents();
    await loadCalendar();
    showToast(id ? "Event updated" : "Event created");
  } catch (error) {
    els.eventError.textContent = error.message;
  } finally {
    els.saveEvent.disabled = false;
  }
});

els.deleteEvent.addEventListener("click", async () => {
  const id = Number(els.eventId.value);
  const event = state.events.find((candidate) => candidate.id === id);
  if (!event || !window.confirm(`Delete “${event.name}” and all of its calendar marks?`)) return;
  try {
    await api(`/api/events/${id}`, { method: "DELETE" });
    els.eventDialog.close();
    state.selectedEventId = null;
    await loadEvents();
    await loadCalendar();
    showToast("Event deleted");
  } catch (error) {
    els.eventError.textContent = error.message;
  }
});

document.querySelectorAll("[data-open-event]").forEach((button) => button.addEventListener("click", () => openEventDialog()));
document.querySelectorAll("[data-close]").forEach((button) => button.addEventListener("click", () => button.closest("dialog").close()));
document.querySelector("#previous-month").addEventListener("click", () => changeMonth(-1));
document.querySelector("#next-month").addEventListener("click", () => changeMonth(1));
document.querySelector("#today-button").addEventListener("click", () => {
  const now = new Date();
  state.month = new Date(now.getFullYear(), now.getMonth(), 1);
  loadCalendar().catch(handleError);
});

function changeMonth(amount) {
  state.month = new Date(state.month.getFullYear(), state.month.getMonth() + amount, 1);
  loadCalendar().catch(handleError);
}

function friendlyDate(value, options = { month: "short", day: "numeric", year: "numeric" }) {
  const [year, month, day] = value.split("-").map(Number);
  return new Date(year, month - 1, day).toLocaleDateString(undefined, options);
}

function formatAverage(value) {
  return Number(value).toLocaleString(undefined, { maximumFractionDigits: 1 });
}

function showToast(message) {
  els.toast.textContent = message;
  els.toast.classList.add("show");
  clearTimeout(showToast.timer);
  showToast.timer = setTimeout(() => els.toast.classList.remove("show"), 2200);
}

function handleError(error) {
  console.error(error);
  showToast(error.message || "Something went wrong");
}

function escapeHTML(value) {
  return String(value).replace(/[&<>'"]/g, (character) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" })[character]);
}

async function init() {
  buildColorOptions();
  try {
    await loadEvents();
    await loadCalendar();
  } catch (error) {
    handleError(error);
    els.calendar.innerHTML = `<p class="empty-events">Could not load the calendar. Refresh to try again.</p>`;
  }
}

init();
