const state = {
  mode: "login",
  token: localStorage.getItem("gochat_token") || "",
  user: JSON.parse(localStorage.getItem("gochat_user") || "null"),
  rooms: [],
  allUsers: [],
  globalOnline: [],
  activeRoomId: null,
  unreadCounts: {},
  oldestMessageId: null,
  hasMore: false,
  searchQuery: "",
  typingUsers: new Set(),
  typingTimer: null,
  isTyping: false,
  ws: null,
  reconnectTimer: null,
};

const QUICK_EMOJIS = ["👍", "❤️", "😂", "🎉", "🔥"];

const el = {
  authView: document.getElementById("auth-view"),
  chatView: document.getElementById("chat-view"),
  tabLogin: document.getElementById("tab-login"),
  tabRegister: document.getElementById("tab-register"),
  authForm: document.getElementById("auth-form"),
  authSubmit: document.getElementById("auth-submit"),
  authError: document.getElementById("auth-error"),
  usernameInput: document.getElementById("username"),
  passwordInput: document.getElementById("password"),
  roomList: document.getElementById("room-list"),
  dmList: document.getElementById("dm-list"),
  createRoomForm: document.getElementById("create-room-form"),
  newRoomName: document.getElementById("new-room-name"),
  activeRoomTitle: document.getElementById("active-room-title"),
  onlineCount: document.getElementById("online-count"),
  onlineUsersList: document.getElementById("online-users-list"),
  allUsersList: document.getElementById("all-users-list"),
  messages: document.getElementById("messages"),
  loadMoreWrap: document.getElementById("load-more-wrap"),
  loadMoreBtn: document.getElementById("load-more-btn"),
  searchForm: document.getElementById("search-form"),
  searchInput: document.getElementById("search-input"),
  clearSearchBtn: document.getElementById("clear-search-btn"),
  typingIndicator: document.getElementById("typing-indicator"),
  emojiBar: document.getElementById("emoji-bar"),
  emojiToggleBtn: document.getElementById("emoji-toggle-btn"),
  messageForm: document.getElementById("message-form"),
  messageInput: document.getElementById("message-input"),
  currentUsername: document.getElementById("current-username"),
  currentUserAvatar: document.getElementById("current-user-avatar"),
  connBadge: document.getElementById("conn-badge"),
  logoutBtn: document.getElementById("logout-btn"),
  toast: document.getElementById("toast"),
};

function showToast(msg) {
  el.toast.textContent = msg;
  el.toast.classList.remove("hidden");
  setTimeout(() => el.toast.classList.add("hidden"), 3000);
}

function userColor(username) {
  let hash = 0;
  for (let i = 0; i < username.length; i++) {
    hash = username.charCodeAt(i) + ((hash << 5) - hash);
  }
  const hue = Math.abs(hash) % 360;
  return `hsl(${hue}, 70%, 65%)`;
}

function setMode(mode) {
  state.mode = mode;
  el.tabLogin.classList.toggle("active", mode === "login");
  el.tabRegister.classList.toggle("active", mode === "register");
  el.authSubmit.textContent = mode === "login" ? "Log In" : "Create Account";
  el.authError.classList.add("hidden");
}

el.tabLogin.addEventListener("click", () => setMode("login"));
el.tabRegister.addEventListener("click", () => setMode("register"));

async function apiFetch(path, options = {}) {
  const headers = { "Content-Type": "application/json", ...(options.headers || {}) };
  if (state.token) {
    headers.Authorization = `Bearer ${state.token}`;
  }
  const res = await fetch(path, { ...options, headers });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(data.error || "Request failed");
  }
  return data;
}

el.authForm.addEventListener("submit", async (e) => {
  e.preventDefault();
  el.authError.classList.add("hidden");
  const endpoint = state.mode === "login" ? "/api/auth/login" : "/api/auth/register";
  try {
    const data = await apiFetch(endpoint, {
      method: "POST",
      body: JSON.stringify({
        username: el.usernameInput.value.trim(),
        password: el.passwordInput.value,
      }),
    });
    state.token = data.token;
    state.user = data.user;
    localStorage.setItem("gochat_token", data.token);
    localStorage.setItem("gochat_user", JSON.stringify(data.user));
    await enterChat();
  } catch (err) {
    el.authError.textContent = err.message;
    el.authError.classList.remove("hidden");
  }
});

el.logoutBtn.addEventListener("click", () => {
  localStorage.removeItem("gochat_token");
  localStorage.removeItem("gochat_user");
  state.token = "";
  state.user = null;
  state.activeRoomId = null;
  if (state.ws) {
    state.ws.close();
    state.ws = null;
  }
  clearTimeout(state.reconnectTimer);
  el.chatView.classList.add("hidden");
  el.authView.classList.remove("hidden");
});

async function enterChat() {
  el.authView.classList.add("hidden");
  el.chatView.classList.remove("hidden");
  el.currentUsername.textContent = state.user.username;
  el.currentUserAvatar.textContent = state.user.username[0] || "U";
  el.currentUserAvatar.style.background = userColor(state.user.username);

  await Promise.all([loadRooms(), loadUsers()]);
  if (state.rooms.length > 0) {
    await selectRoom(state.rooms[0].id);
  }
  connectWS();
}

async function loadRooms() {
  const data = await apiFetch("/api/rooms");
  state.rooms = data.rooms || [];
  renderRooms();
}

async function loadUsers() {
  const data = await apiFetch("/api/users");
  state.allUsers = data.users || [];
  state.globalOnline = data.global_online || [];
  renderAllUsers();
}

function renderRooms() {
  el.roomList.innerHTML = "";
  el.dmList.innerHTML = "";
  for (const room of state.rooms) {
    const li = document.createElement("li");
    li.className = `room-item ${room.id === state.activeRoomId ? "active" : ""}`;

    const label = document.createElement("span");
    label.textContent = room.is_direct ? `@ ${room.name}` : `# ${room.name}`;
    li.appendChild(label);

    const unread = state.unreadCounts[room.id] || 0;
    if (unread > 0) {
      const badge = document.createElement("span");
      badge.className = "unread-badge";
      badge.textContent = unread > 99 ? "99+" : String(unread);
      li.appendChild(badge);
    }

    li.addEventListener("click", () => selectRoom(room.id));
    if (room.is_direct) {
      el.dmList.appendChild(li);
    } else {
      el.roomList.appendChild(li);
    }
  }
}

function renderAllUsers() {
  el.allUsersList.innerHTML = "";
  const onlineSet = new Set(state.globalOnline);
  for (const username of state.allUsers) {
    const li = document.createElement("li");
    li.className = "online-user-item clickable";
    li.title = `Start DM with ${username}`;

    const dot = document.createElement("span");
    dot.className = `status-dot ${onlineSet.has(username) ? "online" : ""}`;
    const name = document.createElement("span");
    name.textContent = username;

    li.appendChild(dot);
    li.appendChild(name);
    li.addEventListener("click", () => startDirectMessage(username));
    el.allUsersList.appendChild(li);
  }
}

async function startDirectMessage(username) {
  try {
    const data = await apiFetch("/api/rooms/dm", {
      method: "POST",
      body: JSON.stringify({ username }),
    });
    await loadRooms();
    if (data.room) {
      await selectRoom(data.room.id);
    }
  } catch (err) {
    showToast(err.message);
  }
}

async function selectRoom(roomId) {
  state.activeRoomId = roomId;
  state.unreadCounts[roomId] = 0;
  state.typingUsers.clear();
  state.searchQuery = "";
  el.searchInput.value = "";
  el.clearSearchBtn.classList.add("hidden");
  renderTyping();

  const room = state.rooms.find((r) => r.id === roomId);
  el.activeRoomTitle.textContent = room
    ? room.is_direct
      ? `@ ${room.name}`
      : `# ${room.name}`
    : "# room";
  renderRooms();

  await fetchMessages(false);

  if (state.ws && state.ws.readyState === WebSocket.OPEN) {
    state.ws.send(JSON.stringify({ type: "join", room_id: roomId }));
  }
}

async function fetchMessages(prepend) {
  const params = new URLSearchParams({ limit: "50" });
  if (prepend && state.oldestMessageId) {
    params.set("before_id", String(state.oldestMessageId));
  }
  if (state.searchQuery) {
    params.set("q", state.searchQuery);
  }

  const data = await apiFetch(`/api/rooms/${state.activeRoomId}/messages?${params.toString()}`);
  const msgs = data.messages || [];
  state.hasMore = Boolean(data.has_more);
  el.loadMoreWrap.classList.toggle("hidden", !state.hasMore);

  if (!prepend) {
    el.messages.innerHTML = "";
    for (const msg of msgs) {
      el.messages.appendChild(buildMessageElement(msg));
    }
    el.messages.scrollTop = el.messages.scrollHeight;
  } else {
    const prevHeight = el.messages.scrollHeight;
    const frag = document.createDocumentFragment();
    for (const msg of msgs) {
      frag.appendChild(buildMessageElement(msg));
    }
    el.messages.prepend(frag);
    el.messages.scrollTop = el.messages.scrollHeight - prevHeight;
  }

  if (msgs.length > 0 && (!prepend || msgs[0].id < state.oldestMessageId)) {
    state.oldestMessageId = msgs[0].id;
  } else if (!prepend && msgs.length === 0) {
    state.oldestMessageId = null;
  }

  renderOnlineUsers(data.online_users || []);
  if (data.global_users) {
    state.globalOnline = data.global_users;
    renderAllUsers();
  }
}

el.loadMoreBtn.addEventListener("click", () => fetchMessages(true));

el.searchForm.addEventListener("submit", async (e) => {
  e.preventDefault();
  state.searchQuery = el.searchInput.value.trim();
  el.clearSearchBtn.classList.toggle("hidden", !state.searchQuery);
  await fetchMessages(false);
});

el.clearSearchBtn.addEventListener("click", async () => {
  state.searchQuery = "";
  el.searchInput.value = "";
  el.clearSearchBtn.classList.add("hidden");
  await fetchMessages(false);
});

el.createRoomForm.addEventListener("submit", async (e) => {
  e.preventDefault();
  const name = el.newRoomName.value.trim();
  if (!name) return;
  try {
    const data = await apiFetch("/api/rooms", {
      method: "POST",
      body: JSON.stringify({ name }),
    });
    el.newRoomName.value = "";
    await loadRooms();
    if (data.room) {
      await selectRoom(data.room.id);
    }
  } catch (err) {
    showToast(err.message);
  }
});

function connectWS() {
  if (!state.token) return;
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  const roomParam = state.activeRoomId ? `&room_id=${state.activeRoomId}` : "";
  const url = `${proto}//${location.host}/ws?token=${encodeURIComponent(state.token)}${roomParam}`;

  const ws = new WebSocket(url);
  state.ws = ws;

  ws.onopen = () => {
    el.connBadge.textContent = "Online";
    el.connBadge.classList.remove("offline");
    el.connBadge.classList.add("online");
  };

  ws.onmessage = (event) => {
    const p = JSON.parse(event.data);

    if (p.type === "error") {
      showToast(p.content || "Error");
      return;
    }
    if (p.type === "global_presence") {
      state.globalOnline = p.global_users || [];
      loadUsers().catch(() => renderAllUsers());
      return;
    }

    if (p.room_id && p.room_id !== state.activeRoomId) {
      if (p.type === "message") {
        state.unreadCounts[p.room_id] = (state.unreadCounts[p.room_id] || 0) + 1;
        renderRooms();
      }
      return;
    }

    if (p.type === "message") {
      state.typingUsers.delete(p.username);
      renderTyping();
      el.messages.appendChild(
        buildMessageElement({
          id: p.message_id,
          room_id: p.room_id,
          user_id: p.user_id,
          username: p.username,
          content: p.content,
          created_at: p.created_at,
          reactions: [],
        })
      );
      el.messages.scrollTop = el.messages.scrollHeight;
    } else if (p.type === "edit") {
      const msgEl = document.getElementById(`msg-${p.message_id}`);
      if (msgEl) {
        const textEl = msgEl.querySelector(".msg-text");
        if (textEl) textEl.textContent = p.content;
        if (!msgEl.querySelector(".msg-edited")) {
          const edited = document.createElement("span");
          edited.className = "msg-edited";
          edited.textContent = "(edited)";
          msgEl.querySelector(".msg-meta").appendChild(edited);
        }
      }
    } else if (p.type === "delete") {
      const msgEl = document.getElementById(`msg-${p.message_id}`);
      if (msgEl) {
        const textEl = msgEl.querySelector(".msg-text");
        if (textEl) {
          textEl.className = "msg-deleted";
          textEl.textContent = "Message deleted";
        }
        const actions = msgEl.querySelector(".msg-actions");
        if (actions) actions.remove();
      }
    } else if (p.type === "react") {
      const msgEl = document.getElementById(`msg-${p.message_id}`);
      if (msgEl) {
        renderReactionsOnElement(msgEl, p.message_id, p.reactions || []);
      }
    } else if (p.type === "typing") {
      state.typingUsers.add(p.username);
      renderTyping();
    } else if (p.type === "stop_typing") {
      state.typingUsers.delete(p.username);
      renderTyping();
    } else if (p.type === "system") {
      appendSystemMessage(p.content);
    } else if (p.type === "presence") {
      renderOnlineUsers(p.online_users || []);
      if (p.global_users) {
        state.globalOnline = p.global_users;
        renderAllUsers();
      }
    }
  };

  ws.onclose = () => {
    el.connBadge.textContent = "Offline";
    el.connBadge.classList.remove("online");
    el.connBadge.classList.add("offline");
    if (state.token) {
      clearTimeout(state.reconnectTimer);
      state.reconnectTimer = setTimeout(connectWS, 2000);
    }
  };
}

function sendWS(payload) {
  if (state.ws && state.ws.readyState === WebSocket.OPEN) {
    state.ws.send(JSON.stringify(payload));
  }
}

el.messageInput.addEventListener("input", () => {
  if (!state.isTyping) {
    state.isTyping = true;
    sendWS({ type: "typing" });
  }
  clearTimeout(state.typingTimer);
  state.typingTimer = setTimeout(() => {
    state.isTyping = false;
    sendWS({ type: "stop_typing" });
  }, 1500);
});

el.messageInput.addEventListener("keydown", (e) => {
  if (e.key === "Enter" && !e.shiftKey) {
    e.preventDefault();
    el.messageForm.requestSubmit();
  }
});

el.messageForm.addEventListener("submit", (e) => {
  e.preventDefault();
  const content = el.messageInput.value.trim();
  if (!content) return;
  sendWS({ type: "message", content });
  el.messageInput.value = "";
  if (state.isTyping) {
    state.isTyping = false;
    clearTimeout(state.typingTimer);
    sendWS({ type: "stop_typing" });
  }
});

el.emojiToggleBtn.addEventListener("click", () => {
  el.emojiBar.classList.toggle("hidden");
});

document.querySelectorAll(".emoji-pick").forEach((btn) => {
  btn.addEventListener("click", () => {
    el.messageInput.value += btn.textContent;
    el.messageInput.focus();
  });
});

function renderTyping() {
  const list = Array.from(state.typingUsers);
  if (list.length === 0) {
    el.typingIndicator.textContent = "";
  } else if (list.length === 1) {
    el.typingIndicator.textContent = `${list[0]} is typing...`;
  } else {
    el.typingIndicator.textContent = `${list.join(", ")} are typing...`;
  }
}

function buildMessageElement(msg) {
  const isOwn = state.user && msg.username === state.user.username;
  const isDeleted = Boolean(msg.deleted_at);

  const div = document.createElement("div");
  div.className = `msg ${isOwn ? "own" : ""}`;
  div.id = `msg-${msg.id}`;

  const meta = document.createElement("div");
  meta.className = "msg-meta";

  const author = document.createElement("span");
  author.className = "msg-author";
  author.textContent = msg.username;
  author.style.color = userColor(msg.username);

  const time = document.createElement("span");
  time.className = "msg-time";
  time.textContent = msg.created_at
    ? new Date(msg.created_at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
    : "";

  meta.appendChild(author);
  meta.appendChild(time);

  if (msg.edited_at && !isDeleted) {
    const edited = document.createElement("span");
    edited.className = "msg-edited";
    edited.textContent = "(edited)";
    meta.appendChild(edited);
  }

  div.appendChild(meta);

  const text = document.createElement("div");
  if (isDeleted) {
    text.className = "msg-deleted";
    text.textContent = "Message deleted";
  } else {
    text.className = "msg-text";
    text.textContent = msg.content;
  }
  div.appendChild(text);

  if (!isDeleted && msg.id) {
    const actions = document.createElement("div");
    actions.className = "msg-actions";
    for (const emoji of QUICK_EMOJIS) {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "msg-action-btn";
      btn.textContent = emoji;
      btn.addEventListener("click", () => sendWS({ type: "react", message_id: msg.id, emoji }));
      actions.appendChild(btn);
    }
    if (isOwn) {
      const editBtn = document.createElement("button");
      editBtn.type = "button";
      editBtn.className = "msg-action-btn";
      editBtn.textContent = "Edit";
      editBtn.addEventListener("click", () => {
        const updated = prompt("Edit message:", text.textContent);
        if (updated && updated.trim()) {
          sendWS({ type: "edit", message_id: msg.id, content: updated.trim() });
        }
      });
      actions.appendChild(editBtn);

      const delBtn = document.createElement("button");
      delBtn.type = "button";
      delBtn.className = "msg-action-btn";
      delBtn.textContent = "Del";
      delBtn.addEventListener("click", () => {
        if (confirm("Delete this message?")) {
          sendWS({ type: "delete", message_id: msg.id });
        }
      });
      actions.appendChild(delBtn);
    }
    div.appendChild(actions);
  }

  const reactionsWrap = document.createElement("div");
  reactionsWrap.className = "msg-reactions";
  div.appendChild(reactionsWrap);
  renderReactionsOnElement(div, msg.id, msg.reactions || []);

  return div;
}

function renderReactionsOnElement(msgEl, messageId, reactions) {
  const wrap = msgEl.querySelector(".msg-reactions");
  if (!wrap) return;
  wrap.innerHTML = "";
  for (const r of reactions) {
    const pill = document.createElement("button");
    pill.type = "button";
    const reacted = state.user && (r.users || []).includes(state.user.username);
    pill.className = `reaction-pill ${reacted ? "reacted" : ""}`;
    pill.textContent = `${r.emoji} ${r.count}`;
    pill.title = (r.users || []).join(", ");
    pill.addEventListener("click", () => sendWS({ type: "react", message_id: messageId, emoji: r.emoji }));
    wrap.appendChild(pill);
  }
}

function appendSystemMessage(text) {
  const div = document.createElement("div");
  div.className = "msg-system";
  div.textContent = text;
  el.messages.appendChild(div);
  el.messages.scrollTop = el.messages.scrollHeight;
}

function renderOnlineUsers(users) {
  el.onlineCount.textContent = `${users.length} online in room`;
  el.onlineUsersList.innerHTML = "";
  for (const username of users) {
    const li = document.createElement("li");
    li.className = "online-user-item";
    const dot = document.createElement("span");
    dot.className = "status-dot online";
    const name = document.createElement("span");
    name.textContent = username;
    li.appendChild(dot);
    li.appendChild(name);
    el.onlineUsersList.appendChild(li);
  }
}

(async function init() {
  if (state.token && state.user) {
    try {
      await apiFetch("/api/auth/me");
      await enterChat();
    } catch {
      localStorage.removeItem("gochat_token");
      localStorage.removeItem("gochat_user");
      state.token = "";
      state.user = null;
    }
  }
})();
