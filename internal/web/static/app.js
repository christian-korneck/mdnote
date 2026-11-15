(() => {
  const state = {
    notes: [],
    total: 0,
    limit: 100,
    offset: 0,
    loadingMore: false,
    query: "",
    mode: "fulltext", // "fulltext" | "meta"
    activeTag: "",
    allTags: [],
    activeFolder: "",
    allFolders: [],
    sort: "updated_desc",
    currentPath: "",
    dirty: false,
  };

  // DOM Elements
  const searchInput = document.getElementById("search-input");
  const searchModeBtn = document.getElementById("search-mode-btn");
  const folderPicker = document.getElementById("folder-picker");
  const folderFilterInput = document.getElementById("folder-filter-input");
  const clearFolderBtn = document.getElementById("clear-folder-btn");
  const folderDropdown = document.getElementById("folder-dropdown");
  const newFolderBtn = document.getElementById("new-folder-btn");
  const sortSelect = document.getElementById("sort-select");
  const tagsLabel = document.getElementById("tags-label");
  const tagFilterInput = document.getElementById("tag-filter-input");
  const tagsList = document.getElementById("tags-list");
  const clearTagBtn = document.getElementById("clear-tag-btn");
  const noteCountEl = document.getElementById("note-count");
  const searchTimingEl = document.getElementById("search-timing");
  const noteListEl = document.getElementById("note-list");
  const newNoteBtn = document.getElementById("new-note-btn");
  const refreshBtn = document.getElementById("refresh-btn");
  const themeBtn = document.getElementById("theme-btn");

  const pathInput = document.getElementById("note-path-input");
  const renameBtn = document.getElementById("rename-btn");
  const activeNoteTags = document.getElementById("active-note-tags");
  const saveStatus = document.getElementById("save-status");
  const saveBtn = document.getElementById("save-btn");
  const popoutBtn = document.getElementById("popout-btn");
  const tabBar = document.getElementById("tab-bar");
  const copyPathBtn = document.getElementById("copy-path-btn");
  const autosaveBtn = document.getElementById("autosave-btn");
  const deleteBtn = document.getElementById("delete-btn");
  const panesEl = document.getElementById("panes");
  const paneResizer = document.getElementById("pane-resizer");
  const editorEl = document.getElementById("editor");
  const previewPane = document.getElementById("preview-pane");
  const previewEl = document.getElementById("preview");

  const uiDialog = document.getElementById("ui-dialog");
  const uiDialogMessage = document.getElementById("ui-dialog-message");
  const uiDialogInput = document.getElementById("ui-dialog-input");
  const uiDialogCancel = document.getElementById("ui-dialog-cancel");
  const uiDialogOk = document.getElementById("ui-dialog-ok");

  // True inside the mdnote-gui desktop app (Wails serves the page from wails://localhost
  // on Linux/macOS and http://wails.localhost on Windows). There, links must not navigate
  // the window.
  const IS_DESKTOP = location.protocol === "wails:" || location.hostname === "wails.localhost";

  // Pop-out window: /?popout=<path> shows just that note, without sidebar or tabs.
  const POPOUT_PATH = new URLSearchParams(location.search).get("popout") || "";

  let searchTimer = null;
  let previewTimer = null;
  let autosaveTimer = null;
  // Saved settings (theme, split divider, autosave) live in the shared config file. The
  // server puts them on <html> when it serves the page; changes go to PATCH /api/settings.
  let autosave = document.documentElement.dataset.autosave !== "false";
  let syncingScroll = false;

  function saveSettings(patch) {
    api("/api/settings", {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(patch),
    }).catch((err) => {
      saveStatus.textContent = "Settings not saved";
      console.error("Saving settings failed:", err);
    });
  }

  async function api(url, options = {}) {
    const res = await fetch(url, options);
    if (!res.ok) {
      const errText = await res.text();
      throw new Error(errText || `HTTP ${res.status}`);
    }
    if (res.status === 204) return null;
    return res.json();
  }

  function encodeNotePath(p) {
    return p.split("/").map(encodeURIComponent).join("/");
  }

  function escapeHTML(str) {
    return String(str)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  // In-page dialogs. Never use the native alert/confirm/prompt: WKWebView under Wails
  // ignores them on macOS (prompt returns null, confirm returns false).
  // showDialog resolves to the input value (or null on cancel) when `input` is a string,
  // otherwise to true (OK) / false (cancel). Calls are queued so dialogs never overlap.
  let dialogQueue = Promise.resolve();

  function showDialog({ message, input = null, okLabel = "OK", danger = false, cancel = true }) {
    const run = () =>
      new Promise((resolve) => {
        const isPrompt = typeof input === "string";
        uiDialogMessage.textContent = message;
        uiDialogInput.classList.toggle("hidden", !isPrompt);
        uiDialogInput.value = isPrompt ? input : "";
        uiDialogCancel.classList.toggle("hidden", !cancel);
        uiDialogOk.textContent = okLabel;
        uiDialogOk.classList.toggle("btn-primary", !danger);
        uiDialogOk.classList.toggle("btn-danger", danger);
        // Escape closes without touching returnValue, so clear any stale "ok".
        uiDialog.returnValue = "";
        uiDialog.addEventListener(
          "close",
          () => {
            const ok = uiDialog.returnValue === "ok";
            resolve(isPrompt ? (ok ? uiDialogInput.value : null) : ok);
          },
          { once: true }
        );
        uiDialog.showModal();
        if (isPrompt) {
          uiDialogInput.focus();
          uiDialogInput.setSelectionRange(input.length, input.length);
        } else if (danger) {
          uiDialogCancel.focus();
        } else {
          uiDialogOk.focus();
        }
      });
    dialogQueue = dialogQueue.then(run, run);
    return dialogQueue;
  }

  const uiAlert = (message) => showDialog({ message, cancel: false });
  const uiConfirm = (message, okLabel = "OK") => showDialog({ message, okLabel, danger: true });
  const uiPrompt = (message, defaultValue = "") => showDialog({ message, input: defaultValue });

  uiDialogCancel.addEventListener("click", () => uiDialog.close("cancel"));

  function formatDate(iso) {
    if (!iso) return "";
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return "";
    return d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
  }

  // Load notes with current filters
  async function loadNotes(reset = true) {
    if (reset) {
      state.offset = 0;
      state.notes = [];
    }
    const params = new URLSearchParams({
      q: state.query,
      mode: state.mode,
      tag: state.activeTag,
      folder: state.activeFolder,
      sort: state.sort,
      limit: String(state.limit),
      offset: String(state.offset),
    });

    try {
      const data = await api(`/api/notes?${params.toString()}`);
      state.total = data.total || 0;
      if (reset) {
        state.notes = data.items || [];
      } else {
        state.notes = state.notes.concat(data.items || []);
      }
      noteCountEl.textContent = `${state.total.toLocaleString()} note${state.total === 1 ? "" : "s"}`;
      searchTimingEl.textContent = data.durationMs != null ? `${data.durationMs.toFixed(1)} ms` : "";
      renderNoteList(reset);
    } catch (err) {
      console.error("Failed to load notes:", err);
    }
  }

  function renderNoteList(scrollTop = false) {
    if (state.notes.length === 0) {
      noteListEl.innerHTML = `<div style="padding:16px 12px;color:var(--text-muted);font-size:12.5px;">No matching notes found.</div>`;
      return;
    }

    noteListEl.innerHTML = state.notes
      .map((n) => {
        const isActive = n.path === state.currentPath ? "active" : "";
        const tagsHTML = (n.tags || [])
          .slice(0, 5)
          .map((t) => `<span class="note-item-tag">#${escapeHTML(t)}</span>`)
          .join("");

        let matchesHTML = "";
        if (n.matches && n.matches.length > 0) {
          const lines = n.matches
            .map(
              (m) =>
                `<div class="note-match-line"><span class="note-match-num">L${m.line}:</span>${escapeHTML(m.snippet)}</div>`
            )
            .join("");
          matchesHTML = `<div class="note-matches">${lines}</div>`;
        }

        return `
          <div class="note-item ${isActive}" data-path="${escapeHTML(n.path)}">
            <button type="button" class="note-item-newtab" title="Open in new tab" aria-label="Open in new tab">+</button>
            <div class="note-item-title">${escapeHTML(n.title || n.path)}</div>
            <div class="note-item-sub">
              <span class="note-item-path">${escapeHTML(n.path)}</span>
              <span>${formatDate(n.updatedAt)}</span>
            </div>
            ${tagsHTML ? `<div class="note-item-tags">${tagsHTML}</div>` : ""}
            ${matchesHTML}
          </div>
        `;
      })
      .join("");

    if (scrollTop) {
      noteListEl.scrollTop = 0;
    }
  }

  async function loadTagsAndFolders() {
    try {
      const [tags, folders] = await Promise.all([api("/api/tags"), api("/api/folders")]);
      renderTags(tags || []);
      renderFolders(folders || []);
    } catch (err) {
      console.error("Failed to load metadata:", err);
    }
  }

  function renderTags(tags) {
    if (tags) {
      state.allTags = tags;
    }
    const all = state.allTags || [];
    tagsLabel.textContent = all.length > 0 ? `Tags (${all.length})` : "Tags";
    clearTagBtn.classList.toggle("hidden", !state.activeTag);

    if (all.length === 0) {
      tagsList.innerHTML = `<span style="font-size:11px;color:var(--text-muted);">Add #tags in notes</span>`;
      return;
    }

    const q = (tagFilterInput.value || "").trim().replace(/^#/, "").toLowerCase();
    const matched = q ? all.filter((t) => t.tag.toLowerCase().includes(q)) : all;

    if (matched.length === 0) {
      tagsList.innerHTML = `<span style="font-size:11px;color:var(--text-muted);">No tags matching "${escapeHTML(tagFilterInput.value)}"</span>`;
      return;
    }

    tagsList.innerHTML = matched
      .map((t) => {
        const active = t.tag === state.activeTag ? "active" : "";
        return `<button type="button" class="tag-pill ${active}" data-tag="${escapeHTML(t.tag)}">#${escapeHTML(t.tag)} <span class="tag-count">${t.count}</span></button>`;
      })
      .join("");
  }

  function renderFolders(folders) {
    state.allFolders = folders || [];
    syncFolderInputDisplay();
    if (!folderDropdown.classList.contains("hidden")) {
      renderFolderDropdown(folderFilterInput.value);
    }
  }

  function syncFolderInputDisplay() {
    folderFilterInput.value = state.activeFolder || "";
    folderFilterInput.placeholder =
      state.allFolders.length > 0
        ? `All folders (${state.allFolders.length}) — filter...`
        : "All folders";
    clearFolderBtn.classList.toggle("hidden", !state.activeFolder);
  }

  function renderFolderDropdown(filterText = "") {
    const q = filterText.trim().toLowerCase();
    const matched = q
      ? state.allFolders.filter((f) => f.toLowerCase().includes(q))
      : state.allFolders;

    let html = "";
    if (!q) {
      const allActive = state.activeFolder === "" ? "active" : "";
      html += `<div class="folder-option ${allActive}" data-folder="">All folders (${state.allFolders.length})</div>`;
    }
    if (matched.length === 0) {
      html += `<div class="folder-option-meta">No folders matching "${escapeHTML(filterText)}"</div>`;
    } else {
      html += matched
        .map((f) => {
          const active = f === state.activeFolder ? "active" : "";
          return `<div class="folder-option ${active}" data-folder="${escapeHTML(f)}">&#128193; ${escapeHTML(f)}</div>`;
        })
        .join("");
    }
    folderDropdown.innerHTML = html;
  }

  function selectFolderFilter(folder) {
    state.activeFolder = folder || "";
    syncFolderInputDisplay();
    folderDropdown.classList.add("hidden");
    loadNotes(true);
  }

  // Before leaving the current note: save it with autosave on, otherwise ask to discard unsaved changes.
  async function leaveCurrentNote() {
    if (!state.dirty || !state.currentPath) return true;
    if (autosave) {
      await saveCurrentNote();
      return true;
    }
    if (!(await uiConfirm(`Discard unsaved changes to "${state.currentPath}"?`, "Discard"))) return false;
    state.dirty = false;
    return true;
  }

  // In-app tabs. The active tab's live state is the editor plus state.currentPath and
  // state.dirty, so saving, renaming, deleting and autosave work on it unchanged. Inactive
  // tabs keep a snapshot, so unsaved text survives switching tabs. Open notes with openNote().
  const tabs = []; // { id, path, content, dirty, tags, scrollTop }
  let activeTabId = null;
  let nextTabId = 1;

  const activeTab = () => tabs.find((t) => t.id === activeTabId) || null;
  const tabPath = (t) => (t.id === activeTabId ? state.currentPath : t.path);
  const tabDirty = (t) => (t.id === activeTabId ? state.dirty : t.dirty);

  function renderTabs() {
    tabBar.innerHTML = tabs
      .map((t) => {
        const active = t.id === activeTabId;
        const path = tabPath(t);
        const name = path.split("/").pop() || "untitled";
        return `<div class="tab${active ? " active" : ""}${tabDirty(t) ? " dirty" : ""}" role="tab" aria-selected="${active}" data-id="${t.id}" title="${escapeHTML(path)}">
            <span class="tab-title">${escapeHTML(name)}</span>
            <button type="button" class="tab-close" title="Close tab" aria-label="Close tab">&times;</button>
          </div>`;
      })
      .join("");
    tabBar.classList.toggle("hidden", tabs.length === 0);
  }

  // Copy the live editor state into the active tab (creating one if a note was saved
  // with no tab open) and redraw the tab bar.
  function syncActiveTab() {
    let tab = activeTab();
    if (!tab && state.currentPath) {
      tab = { id: nextTabId++, path: "", content: "", dirty: false, tags: [], scrollTop: 0 };
      tabs.push(tab);
      activeTabId = tab.id;
    }
    if (tab) {
      tab.path = state.currentPath;
      tab.dirty = state.dirty;
    }
    if (POPOUT_PATH) document.title = state.currentPath ? `${state.currentPath} – mdnote` : "mdnote";
    renderTabs();
  }

  // Absolute file path of the open note: the server puts the notes directory and the
  // OS path separator on <html>.
  const NOTES_DIR = document.documentElement.dataset.notesDir || "";
  const PATH_SEP = document.documentElement.dataset.pathSep || "/";

  function absolutePath(rel) {
    const dir = NOTES_DIR.endsWith(PATH_SEP) ? NOTES_DIR : NOTES_DIR + PATH_SEP;
    return dir + rel.split("/").join(PATH_SEP);
  }

  // Copy the open note's absolute path. The desktop app uses the native clipboard
  // (/api/copy-text); WebKit only allows script copies during real user gestures. In the
  // browser the Clipboard API needs a secure context (localhost or https), so fall back to
  // copying from a temporary, selected textarea.
  let copyResetTimer = null;
  async function copyFilePath() {
    if (!state.currentPath || !NOTES_DIR) return;
    const text = absolutePath(state.currentPath);
    let ok = false;
    try {
      if (IS_DESKTOP) {
        await api("/api/copy-text", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ text }),
        });
      } else {
        await navigator.clipboard.writeText(text);
      }
      ok = true;
    } catch {
      const ta = document.createElement("textarea");
      ta.value = text;
      ta.setAttribute("readonly", "");
      ta.style.cssText = "position:fixed;top:0;left:0;opacity:0;";
      document.body.appendChild(ta);
      ta.select();
      try {
        ok = document.execCommand("copy");
      } catch {
        ok = false;
      }
      ta.remove();
    }
    copyPathBtn.textContent = ok ? "Copied" : "Copy failed";
    clearTimeout(copyResetTimer);
    copyResetTimer = setTimeout(() => (copyPathBtn.textContent = "Copy path"), 1500);
  }

  function snapshotActiveTab() {
    const tab = activeTab();
    if (!tab) return;
    tab.path = state.currentPath;
    tab.dirty = state.dirty;
    tab.content = editorEl.value;
    tab.tags = currentTags;
    tab.scrollTop = editorEl.scrollTop;
  }

  // Show a note loaded from the API in the editor (the active tab).
  function showNote(note) {
    state.currentPath = note.path;
    state.dirty = false;
    pathInput.value = note.path;
    renameBtn.classList.add("hidden");
    editorEl.value = note.content || "";
    previewEl.innerHTML = note.html || "";
    renderActiveNoteTags(note.tags || []);
    saveStatus.textContent = "Saved";
    highlightSelectedNoteInSidebar();
    syncActiveTab();
  }

  function clearEditor() {
    state.currentPath = "";
    state.dirty = false;
    pathInput.value = "";
    renameBtn.classList.add("hidden");
    editorEl.value = "";
    previewEl.innerHTML = "";
    renderActiveNoteTags([]);
    highlightSelectedNoteInSidebar();
    syncActiveTab();
  }

  // Before showing another tab: with autosave on, save unsaved changes. The tab keeps its
  // text either way, so with autosave off nothing is lost and there's no prompt.
  async function leaveActiveTab() {
    if (!activeTab()) return;
    if (autosave && state.dirty && state.currentPath) await saveCurrentNote();
    snapshotActiveTab();
  }

  // Open a note: switch to its tab if it's already open; open a new tab when asked (or
  // when no tab is open); otherwise show it in the current tab.
  async function openNote(path, { newTab = false } = {}) {
    const existing = tabs.find((t) => tabPath(t) === path);
    if (existing) return switchToTab(existing.id);

    const inNewTab = newTab || !activeTab();
    if (inNewTab) await leaveActiveTab();
    else if (!(await leaveCurrentNote())) return;

    let note;
    try {
      note = await api(`/api/notes/${encodeNotePath(path)}`);
    } catch (err) {
      saveStatus.textContent = "Could not open note";
      console.error("Failed to open note:", err);
      return;
    }
    if (inNewTab) {
      const tab = { id: nextTabId++, path: note.path, content: "", dirty: false, tags: [], scrollTop: 0 };
      tabs.push(tab);
      activeTabId = tab.id;
    }
    clearTimeout(autosaveTimer);
    showNote(note);
    editorEl.scrollTop = 0;
  }

  async function switchToTab(id) {
    const tab = tabs.find((t) => t.id === id);
    if (!tab || id === activeTabId) return;
    await leaveActiveTab();
    activeTabId = id;
    await showTab(tab);
  }

  // Show a tab in the editor: a clean tab is re-read from disk (so changes made elsewhere
  // show up), a dirty one gets its unsaved text back.
  async function showTab(tab) {
    clearTimeout(autosaveTimer);
    if (tab.dirty) {
      state.currentPath = tab.path;
      state.dirty = true;
      pathInput.value = tab.path;
      renameBtn.classList.add("hidden");
      editorEl.value = tab.content;
      editorEl.scrollTop = tab.scrollTop;
      renderActiveNoteTags(tab.tags);
      saveStatus.textContent = "Modified";
      highlightSelectedNoteInSidebar();
      syncActiveTab();
      await renderLivePreview();
      return;
    }
    let note;
    try {
      note = await api(`/api/notes/${encodeNotePath(tab.path)}`);
    } catch (err) {
      console.error("Failed to open note:", err);
      await removeTab(tab.id);
      saveStatus.textContent = "Note no longer exists";
      return;
    }
    showNote(note);
    editorEl.scrollTop = tab.scrollTop;
  }

  // Close a tab; unsaved changes are saved (active tab, autosave on) or discarded after asking.
  async function closeTab(id) {
    const tab = tabs.find((t) => t.id === id);
    if (!tab) return;
    if (tabDirty(tab)) {
      if (id === activeTabId && autosave) {
        await saveCurrentNote();
        if (state.dirty) return; // the save failed: keep the tab
      } else if (!(await uiConfirm(`Discard unsaved changes to "${tabPath(tab)}"?`, "Discard"))) {
        return;
      }
    }
    await removeTab(id);
  }

  // Remove a tab without asking. If it was the active one, show its neighbour or an empty editor.
  async function removeTab(id) {
    const idx = tabs.findIndex((t) => t.id === id);
    if (idx < 0) return;
    tabs.splice(idx, 1);
    if (id !== activeTabId) {
      renderTabs();
      return;
    }
    clearTimeout(autosaveTimer);
    state.dirty = false;
    const next = tabs[idx] || tabs[idx - 1];
    if (!next) {
      activeTabId = null;
      clearEditor();
      return;
    }
    activeTabId = next.id;
    await showTab(next);
  }

  // Save an inactive tab's unsaved text (used when autosave is turned back on).
  async function saveTabContent(tab) {
    try {
      await api(`/api/notes/${encodeNotePath(tab.path)}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ content: tab.content }),
      });
      tab.dirty = false;
      renderTabs();
    } catch (err) {
      saveStatus.textContent = "Error saving";
      console.error("Save error:", err);
    }
  }

  // Move the current note into its own window (browser pop-up, or a new window in the
  // desktop app) and close its tab here. Unsaved changes are saved first.
  async function popOutCurrentNote() {
    if (!state.currentPath) return;
    if (state.dirty) {
      if (!autosave && !(await showDialog({ message: `Save changes to "${state.currentPath}" and pop it out?`, okLabel: "Save & pop out" }))) {
        return;
      }
      await saveCurrentNote();
      if (state.dirty) return; // the save failed
    }
    const path = state.currentPath;
    if (IS_DESKTOP) {
      try {
        await api("/api/open-window", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ path }),
        });
      } catch (err) {
        uiAlert(`Could not open a window: ${err.message}`);
        return;
      }
    } else if (!window.open(`/?popout=${encodeURIComponent(path)}`, "_blank", "popup,width=1000,height=800")) {
      uiAlert("The browser blocked the pop-up window. Allow pop-ups for this page and try again.");
      return;
    }
    await removeTab(activeTabId);
  }

  let currentTags = [];
  function renderActiveNoteTags(tags) {
    currentTags = tags || [];
    activeNoteTags.innerHTML = (tags || [])
      .map((t) => `<span class="note-item-tag">#${escapeHTML(t)}</span>`)
      .join("");
  }

  function highlightSelectedNoteInSidebar() {
    document.querySelectorAll(".note-item").forEach((el) => {
      el.classList.toggle("active", el.dataset.path === state.currentPath);
    });
  }

  async function renderLivePreview() {
    try {
      const res = await api("/api/render", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ content: editorEl.value, path: pathInput.value }),
      });
      previewEl.innerHTML = res.html || "";
    } catch (err) {
      console.error("Preview render error:", err);
    }
  }

  async function saveCurrentNote() {
    const targetPath = pathInput.value.trim();
    if (!targetPath) return;

    clearTimeout(autosaveTimer);
    saveStatus.textContent = "Saving...";
    try {
      const saved = await api(`/api/notes/${encodeNotePath(targetPath)}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ content: editorEl.value }),
      });
      state.currentPath = saved.path;
      state.dirty = false;
      pathInput.value = saved.path;
      renameBtn.classList.add("hidden");
      renderActiveNoteTags(saved.tags || []);
      saveStatus.textContent = "Saved";
      syncActiveTab();
      await Promise.all([loadNotes(true), loadTagsAndFolders()]);
    } catch (err) {
      saveStatus.textContent = "Error saving";
      console.error("Save error:", err);
    }
  }

  async function renameCurrentNote() {
    const newPath = pathInput.value.trim();
    if (!newPath || !state.currentPath || newPath === state.currentPath) {
      renameBtn.classList.add("hidden");
      return;
    }
    try {
      const moved = await api("/api/notes/rename", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ oldPath: state.currentPath, newPath }),
      });
      state.currentPath = moved.path;
      pathInput.value = moved.path;
      renameBtn.classList.add("hidden");
      saveStatus.textContent = "Moved";
      syncActiveTab();
      await Promise.all([loadNotes(true), loadTagsAndFolders()]);
    } catch (err) {
      uiAlert(`Rename failed: ${err.message}`);
    }
  }

  async function createNewFolder() {
    const defaultPrefix = state.activeFolder ? `${state.activeFolder}/` : "";
    const input = await uiPrompt("New folder path (e.g. projects or projects/2026):", defaultPrefix);
    if (!input || !input.trim()) return;

    try {
      const res = await api("/api/folders", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ path: input.trim() }),
      });
      state.activeFolder = res.folder;
      await Promise.all([loadTagsAndFolders(), loadNotes(true)]);
    } catch (err) {
      uiAlert(`Could not create folder: ${err.message}`);
    }
  }

  async function createNewNote() {
    const prefix = state.activeFolder ? `${state.activeFolder}/` : "";
    const stamp = new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-");
    const defaultPath = `${prefix}note-${stamp}.md`;
    const initialContent = `# New Note\n\n#notes\n\nStart writing here...\n`;

    try {
      const created = await api("/api/notes", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ path: defaultPath, content: initialContent }),
      });
      await Promise.all([loadNotes(true), loadTagsAndFolders()]);
      await openNote(created.path, { newTab: true });
      editorEl.focus();
    } catch (err) {
      uiAlert(`Could not create note: ${err.message}`);
    }
  }

  async function refreshFromFilesystem() {
    saveStatus.textContent = "Refreshing...";
    try {
      await api("/api/refresh", { method: "POST" });
      await Promise.all([loadTagsAndFolders(), loadNotes(true)]);
      if (state.currentPath && !state.dirty) {
        try {
          const note = await api(`/api/notes/${encodeNotePath(state.currentPath)}`);
          editorEl.value = note.content || "";
          previewEl.innerHTML = note.html || "";
          renderActiveNoteTags(note.tags || []);
        } catch (_) {
          // Active note was deleted on disk
          await removeTab(activeTabId);
        }
      }
      saveStatus.textContent = "Refreshed";
    } catch (err) {
      saveStatus.textContent = "Refresh failed";
      console.error("Refresh error:", err);
    }
  }

  async function deleteCurrentNote() {
    if (!state.currentPath) return;
    if (!(await uiConfirm(`Delete "${state.currentPath}" from disk?`, "Delete"))) return;

    try {
      await api(`/api/notes/${encodeNotePath(state.currentPath)}`, {
        method: "DELETE",
      });
      await removeTab(activeTabId);
      saveStatus.textContent = "Deleted";
      await Promise.all([loadNotes(true), loadTagsAndFolders()]);
    } catch (err) {
      uiAlert(`Delete failed: ${err.message}`);
    }
  }

  // Image Upload helper (Clipboard Paste & Drag-and-Drop)
  async function uploadImageAndInsert(file) {
    const startPos = editorEl.selectionStart;
    const endPos = editorEl.selectionEnd;
    const placeholder = `![Uploading ${file.name || "image"}...]()`;

    const before = editorEl.value.slice(0, startPos);
    const after = editorEl.value.slice(endPos);
    editorEl.value = before + placeholder + after;
    saveStatus.textContent = "Uploading image...";

    // Send the raw bytes rather than FormData/Blob: WebKitGTK under Wails (mdnote-gui)
    // can't pass FormData or Blob request bodies to the in-process handler.
    const params = new URLSearchParams({
      notePath: pathInput.value.trim() || state.currentPath,
      filename: file.name || "pasted-image.png",
    });

    try {
      const res = await api(`/api/assets?${params}`, {
        method: "POST",
        headers: { "Content-Type": "application/octet-stream" },
        body: await file.arrayBuffer(),
      });
      const mdImage = res.markdown || `![image](${res.url})`;
      editorEl.value = editorEl.value.replace(placeholder, mdImage);
      state.dirty = true;
      renderTabs();
      await renderLivePreview();
      if (autosave) await saveCurrentNote();
      else saveStatus.textContent = "Modified";
    } catch (err) {
      editorEl.value = editorEl.value.replace(placeholder, "");
      saveStatus.textContent = "Image upload failed";
      console.error("Asset upload failed:", err);
    }
  }

  // Event Listeners
  searchInput.addEventListener("input", () => {
    state.query = searchInput.value;
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => loadNotes(true), 140);
  });

  searchModeBtn.addEventListener("click", () => {
    state.mode = state.mode === "fulltext" ? "meta" : "fulltext";
    searchModeBtn.dataset.mode = state.mode;
    searchModeBtn.textContent = state.mode === "fulltext" ? "Full-text" : "Title/Meta";
    loadNotes(true);
  });

  folderFilterInput.addEventListener("focus", () => {
    renderFolderDropdown("");
    folderDropdown.classList.remove("hidden");
    folderFilterInput.select();
  });

  folderFilterInput.addEventListener("input", () => {
    renderFolderDropdown(folderFilterInput.value);
    folderDropdown.classList.remove("hidden");
  });

  folderFilterInput.addEventListener("keydown", (e) => {
    if (e.key === "Escape") {
      folderDropdown.classList.add("hidden");
      syncFolderInputDisplay();
      folderFilterInput.blur();
      return;
    }
    if (e.key === "Enter") {
      e.preventDefault();
      const firstOpt = folderDropdown.querySelector(".folder-option");
      if (firstOpt) {
        selectFolderFilter(firstOpt.dataset.folder);
        folderFilterInput.blur();
      }
    }
  });

  clearFolderBtn.addEventListener("click", (e) => {
    e.stopPropagation();
    selectFolderFilter("");
  });

  folderDropdown.addEventListener("mousedown", (e) => {
    const opt = e.target.closest(".folder-option");
    if (!opt) return;
    e.preventDefault();
    selectFolderFilter(opt.dataset.folder);
  });

  document.addEventListener("mousedown", (e) => {
    if (!folderPicker.contains(e.target)) {
      if (!folderDropdown.classList.contains("hidden")) {
        folderDropdown.classList.add("hidden");
        syncFolderInputDisplay();
      }
    }
  });

  newFolderBtn.addEventListener("click", createNewFolder);

  sortSelect.addEventListener("change", () => {
    state.sort = sortSelect.value;
    loadNotes(true);
  });

  tagFilterInput.addEventListener("input", () => {
    renderTags();
  });

  tagFilterInput.addEventListener("keydown", (e) => {
    if (e.key === "Escape") {
      tagFilterInput.value = "";
      renderTags();
      tagFilterInput.blur();
      return;
    }
    if (e.key === "Enter") {
      e.preventDefault();
      const firstPill = tagsList.querySelector(".tag-pill");
      if (firstPill) {
        const clickedTag = firstPill.dataset.tag;
        state.activeTag = state.activeTag === clickedTag ? "" : clickedTag;
        renderTags();
        loadNotes(true);
      }
    }
  });

  tagsList.addEventListener("click", (e) => {
    const pill = e.target.closest(".tag-pill");
    if (!pill) return;
    const clickedTag = pill.dataset.tag;
    state.activeTag = state.activeTag === clickedTag ? "" : clickedTag;
    renderTags();
    loadNotes(true);
  });

  clearTagBtn.addEventListener("click", () => {
    state.activeTag = "";
    renderTags();
    loadNotes(true);
  });

  // Plain click: show the note in the current tab. Ctrl/Cmd+click, middle-click or the
  // "+" button: open it in a new tab.
  noteListEl.addEventListener("click", (e) => {
    const item = e.target.closest(".note-item");
    if (!item) return;
    const newTab = e.ctrlKey || e.metaKey || !!e.target.closest(".note-item-newtab");
    openNote(item.dataset.path, { newTab });
  });
  noteListEl.addEventListener("mousedown", (e) => {
    if (e.button === 1 && e.target.closest(".note-item")) e.preventDefault(); // no autoscroll
  });
  noteListEl.addEventListener("auxclick", (e) => {
    const item = e.target.closest(".note-item");
    if (e.button !== 1 || !item) return;
    e.preventDefault();
    openNote(item.dataset.path, { newTab: true });
  });

  // Tab bar: click switches, the × or a middle-click closes.
  tabBar.addEventListener("click", (e) => {
    const el = e.target.closest(".tab");
    if (!el) return;
    const id = Number(el.dataset.id);
    if (e.target.closest(".tab-close")) closeTab(id);
    else switchToTab(id);
  });
  tabBar.addEventListener("mousedown", (e) => {
    if (e.button === 1) e.preventDefault();
  });
  tabBar.addEventListener("auxclick", (e) => {
    const el = e.target.closest(".tab");
    if (e.button !== 1 || !el) return;
    e.preventDefault();
    closeTab(Number(el.dataset.id));
  });

  // Infinite scroll on sidebar note list for thousands of notes
  noteListEl.addEventListener("scroll", async () => {
    if (state.loadingMore) return;
    if (state.notes.length >= state.total) return;
    const nearBottom =
      noteListEl.scrollTop + noteListEl.clientHeight >= noteListEl.scrollHeight - 120;
    if (nearBottom) {
      state.loadingMore = true;
      state.offset += state.limit;
      await loadNotes(false);
      state.loadingMore = false;
    }
  });

  newNoteBtn.addEventListener("click", createNewNote);
  refreshBtn.addEventListener("click", refreshFromFilesystem);
  saveBtn.addEventListener("click", saveCurrentNote);
  function renderAutosaveBtn() {
    autosaveBtn.textContent = autosave ? "Autosave: On" : "Autosave: Off";
    autosaveBtn.setAttribute("aria-pressed", String(autosave));
    autosaveBtn.title = autosave
      ? "Autosave while typing (click to turn off)"
      : "Autosave is off: save with Save or Ctrl/Cmd+S (click to turn on)";
  }
  renderAutosaveBtn();
  autosaveBtn.addEventListener("click", () => {
    autosave = !autosave;
    renderAutosaveBtn();
    saveSettings({ autosave });
    clearTimeout(autosaveTimer);
    if (!autosave) return;
    if (state.dirty && (state.currentPath || pathInput.value.trim())) saveCurrentNote();
    for (const t of tabs) {
      if (t.id !== activeTabId && t.dirty) saveTabContent(t);
    }
  });
  popoutBtn.addEventListener("click", popOutCurrentNote);
  copyPathBtn.addEventListener("click", copyFilePath);
  window.addEventListener("beforeunload", (e) => {
    if (!autosave && (state.dirty || tabs.some((t) => t.id !== activeTabId && t.dirty))) {
      e.preventDefault();
      e.returnValue = "";
    }
  });
  deleteBtn.addEventListener("click", deleteCurrentNote);
  renameBtn.addEventListener("click", renameCurrentNote);

  pathInput.addEventListener("input", () => {
    const changed = state.currentPath && pathInput.value.trim() !== state.currentPath;
    renameBtn.classList.toggle("hidden", !changed);
  });

  pathInput.addEventListener("keydown", (e) => {
    if (e.key === "Enter") {
      e.preventDefault();
      if (state.currentPath && pathInput.value.trim() !== state.currentPath) {
        renameCurrentNote();
      } else {
        saveCurrentNote();
      }
    }
  });

  editorEl.addEventListener("input", () => {
    if (!state.dirty) {
      state.dirty = true;
      renderTabs();
    }
    saveStatus.textContent = "Modified";
    clearTimeout(previewTimer);
    previewTimer = setTimeout(renderLivePreview, 120);

    if (autosave && (state.currentPath || pathInput.value.trim())) {
      clearTimeout(autosaveTimer);
      autosaveTimer = setTimeout(saveCurrentNote, 900);
    }
  });

  // Tab indentation and Ctrl/Cmd+S in editor
  editorEl.addEventListener("keydown", (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "s") {
      e.preventDefault();
      saveCurrentNote();
      return;
    }
    if (e.key === "Tab") {
      e.preventDefault();
      const start = editorEl.selectionStart;
      const end = editorEl.selectionEnd;
      editorEl.value = editorEl.value.slice(0, start) + "  " + editorEl.value.slice(end);
      editorEl.selectionStart = editorEl.selectionEnd = start + 2;
      editorEl.dispatchEvent(new Event("input"));
    }
  });

  // Copy & Paste Images directly into editor
  editorEl.addEventListener("paste", (e) => {
    const items = e.clipboardData && e.clipboardData.items;
    if (!items) return;
    for (const item of items) {
      if (item.type && item.type.startsWith("image/")) {
        e.preventDefault();
        const file = item.getAsFile();
        if (file) uploadImageAndInsert(file);
        return;
      }
    }
  });

  // Drag & Drop Images into editor
  editorEl.addEventListener("drop", (e) => {
    const files = e.dataTransfer && e.dataTransfer.files;
    if (!files || files.length === 0) return;
    for (const file of files) {
      if (file.type && file.type.startsWith("image/")) {
        e.preventDefault();
        uploadImageAndInsert(file);
        return;
      }
    }
  });

  // View Switcher (Split / Edit / Preview)
  document.querySelectorAll(".view-btn").forEach((btn) => {
    btn.addEventListener("click", () => {
      document.querySelectorAll(".view-btn").forEach((b) => b.classList.remove("active"));
      btn.classList.add("active");
      panesEl.dataset.view = btn.dataset.view;
    });
  });

  // Draggable Split View Divider (5% to 95%, double-click to reset to 50%). The position
  // is saved when a drag ends.
  let isDraggingSplit = false;
  let draggedRatio = null;
  paneResizer.addEventListener("mousedown", (e) => {
    e.preventDefault();
    isDraggingSplit = true;
    draggedRatio = null;
    paneResizer.classList.add("dragging");
    panesEl.classList.add("resizing");
  });

  window.addEventListener("mousemove", (e) => {
    if (!isDraggingSplit) return;
    const rect = panesEl.getBoundingClientRect();
    if (rect.width <= 0) return;
    let pct = ((e.clientX - rect.left) / rect.width) * 100;
    pct = Math.max(5, Math.min(95, pct));
    draggedRatio = Number(pct.toFixed(2));
    panesEl.style.setProperty("--split-ratio", `${draggedRatio}%`);
  });

  window.addEventListener("mouseup", () => {
    if (!isDraggingSplit) return;
    isDraggingSplit = false;
    paneResizer.classList.remove("dragging");
    panesEl.classList.remove("resizing");
    if (draggedRatio !== null) saveSettings({ splitRatio: draggedRatio });
  });

  paneResizer.addEventListener("dblclick", () => {
    panesEl.style.setProperty("--split-ratio", "50%");
    saveSettings({ splitRatio: 50 });
  });

  // Proportional Scroll Sync
  editorEl.addEventListener("scroll", () => {
    if (syncingScroll) return;
    syncingScroll = true;
    const maxEditor = editorEl.scrollHeight - editorEl.clientHeight;
    const maxPreview = previewPane.scrollHeight - previewPane.clientHeight;
    if (maxEditor > 0 && maxPreview > 0) {
      previewPane.scrollTop = (editorEl.scrollTop / maxEditor) * maxPreview;
    }
    requestAnimationFrame(() => {
      syncingScroll = false;
    });
  });

  // Light / Dark / System theme. "system" is the absence of the data-theme attribute, so
  // the CSS follows prefers-color-scheme. The server sets the saved value on <html>
  // before first paint; a change is saved to the config file.
  const THEME_ORDER = ["system", "light", "dark"];
  const THEME_LABELS = { system: "System", light: "Light", dark: "Dark" };

  function getThemePref() {
    const v = document.documentElement.dataset.theme;
    return v === "light" || v === "dark" ? v : "system";
  }

  function applyThemePref(pref) {
    if (pref === "system") {
      document.documentElement.removeAttribute("data-theme");
    } else {
      document.documentElement.setAttribute("data-theme", pref);
    }
    const next = THEME_ORDER[(THEME_ORDER.indexOf(pref) + 1) % THEME_ORDER.length];
    const label = `Theme: ${THEME_LABELS[pref]} (click for ${THEME_LABELS[next]})`;
    themeBtn.dataset.pref = pref;
    themeBtn.title = label;
    themeBtn.setAttribute("aria-label", label);
  }

  themeBtn.addEventListener("click", () => {
    const current = themeBtn.dataset.pref || "system";
    const next = THEME_ORDER[(THEME_ORDER.indexOf(current) + 1) % THEME_ORDER.length];
    applyThemePref(next);
    saveSettings({ theme: next });
  });

  // Links in the preview: open external links outside the app (a new browser tab, or the
  // system browser in the desktop app) so the editor is never navigated away. In-page
  // #anchors work as usual. In the desktop app other links are ignored, since the
  // window has no back button to return from them.
  previewEl.addEventListener("click", (e) => {
    const link = e.target.closest("a[href]");
    if (!link) return;
    const href = link.getAttribute("href");
    if (href.startsWith("#")) return;

    if (/^(https?:|mailto:)/i.test(href)) {
      e.preventDefault();
      if (IS_DESKTOP) {
        api("/api/open-url", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ url: href }),
        }).catch((err) => {
          saveStatus.textContent = "Could not open link";
          console.error("Open URL failed:", err);
        });
      } else {
        window.open(href, "_blank", "noopener");
      }
    } else if (IS_DESKTOP) {
      e.preventDefault();
    }
  });

  applyThemePref(getThemePref());

  // Initial Boot: a pop-out window shows only its note; the main window opens the first
  // note in a tab.
  if (POPOUT_PATH) {
    document.body.classList.add("popout");
    openNote(POPOUT_PATH);
  } else {
    loadTagsAndFolders();
    loadNotes(true).then(() => {
      if (!activeTab() && state.notes.length > 0) openNote(state.notes[0].path);
    });
  }
})();
