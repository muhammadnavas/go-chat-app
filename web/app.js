const state = {
  mode: "login", // "login" | "register"
  token: localStorage.getItem("gochat_token") || "",
  user: JSON.parse(localStorage.getItem("gochat_user") || "null"),
  rooms: [],
  activeRoomId: null,
  ws: null,
  reconnectTimer: null,
};

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
  createRoomForm: document.getElementById("create-room-form"),
  newRoomName: document.getElementById("new-room-name"),
  activeRoomTitle: document.getElementById("active-room-title"),
  onlineCount: document.getElementById("online-count"),
  onlineUsersList: document.getElementById("online-users-list"),
  messages: document.getElementById("messages"),
  messageForm: document.getElementById("message-form"),
  messageInput: document.getElementById("message-input"),
  currentUsername: document.getElementById("current-username"),
  currentUserAvatar: document.getElementById("current-user-avatar"),
  connBadge: document.getElementById("conn-badge"),
  logoutBtn: document.getElementById("logout-btn"),
};

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

  await loadRooms();
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

function renderRooms() {
  el.roomList.innerHTML = "";
  for (const room of state.rooms) {
    const li = document.createElement("li");
    li.className = `room-item ${room.id === state.activeRoomId ? "active" : ""}`;
    li.textContent = `# ${room.name}`;
    li.addEventListener("click", () => selectRoom(room.id));
    el.roomList.appendChild(li);
  }
}

async function selectRoom(roomId) {
  state.activeRoomId = roomId;
  const room = state.rooms.find((r) => r.id === roomId);
  el.activeRoomTitle.textContent = room ? `# ${room.name}` : "# room";
  renderRooms();

  const data = await apiFetch(`/api/rooms/${roomId}/messages?limit=50`);
  el.messages.innerHTML = "";
  for (const msg of data.messages || []) {
    appendMessage(msg);
  }
  renderOnlineUsers(data.online_users || []);

  if (state.ws && state.ws.readyState === WebSocket.OPEN) {
    state.ws.send(JSON.stringify({ type: "join", room_id: roomId }));
  }
}

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
    alert(err.message);
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
    const payload = JSON.parse(event.data);
    if (payload.room_id && payload.room_id !== state.activeRoomId) {
      return;
    }
    if (payload.type === "message") {
      appendMessage(payload);
    } else if (payload.type === "system") {
      appendSystemMessage(payload.content);
    } else if (payload.type === "presence") {
      renderOnlineUsers(payload.online_users || []);
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

el.messageForm.addEventListener("submit", (e) => {
  e.preventDefault();
  const content = el.messageInput.value.trim();
  if (!content || !state.ws || state.ws.readyState !== WebSocket.OPEN) return;
  state.ws.send(JSON.stringify({ type: "message", content }));
  el.messageInput.value = "";
});

function appendMessage(msg) {
  const isOwn = state.user && msg.username === state.user.username;
  const div = document.createElement("div");
  div.className = `msg ${isOwn ? "own" : ""}`;

  const meta = document.createElement("div");
  meta.className = "msg-meta";

  const author = document.createElement("span");
  author.className = "msg-author";
  author.textContent = msg.username;

  const time = document.createElement("span");
  time.className = "msg-time";
  time.textContent = msg.created_at
    ? new Date(msg.created_at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
    : "";

  meta.appendChild(author);
  meta.appendChild(time);

  const text = document.createElement("div");
  text.className = "msg-text";
  text.textContent = msg.content;

  div.appendChild(meta);
  div.appendChild(text);
  el.messages.appendChild(div);
  el.messages.scrollTop = el.messages.scrollHeight;
}

function appendSystemMessage(text) {
  const div = document.createElement("div");
  div.className = "msg-system";
  div.textContent = text;
  el.messages.appendChild(div);
  el.messages.scrollTop = el.messages.scrollHeight;
}

function renderOnlineUsers(users) {
  el.onlineCount.textContent = `${users.length} online`;
  el.onlineUsersList.innerHTML = "";
  for (const username of users) {
    const li = document.createElement("li");
    li.className = "online-user-item";
    const dot = document.createElement("span");
    dot.className = "online-dot";
    const name = document.createElement("span");
    name.textContent = username;
    li.appendChild(dot);
    li.appendChild(name);
    el.onlineUsersList.appendChild(li);
  }
}

// Auto-login if token exists in localStorage
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

