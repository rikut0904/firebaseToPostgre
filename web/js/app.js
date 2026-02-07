const $ = (id) => document.getElementById(id);

function setActiveTab(tabId) {
  document.querySelectorAll(".tab").forEach((btn) => {
    btn.classList.toggle("active", btn.dataset.tab === tabId);
  });
  document.querySelectorAll(".tab-content").forEach((panel) => {
    panel.classList.toggle("active", panel.id === tabId);
  });
}

function initTabs() {
  document.querySelectorAll(".tab").forEach((btn) => {
    btn.addEventListener("click", () => setActiveTab(btn.dataset.tab));
  });
}

function initCredentialsFile() {
  const fileInput = $("credentialsFile");
  const textArea = $("credentialsJson");
  if (!fileInput || !textArea) return;
  fileInput.addEventListener("change", () => {
    const file = fileInput.files && fileInput.files[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = () => {
      textArea.value = reader.result || "";
    };
    reader.readAsText(file);
  });
}

function initWS() {
  const proto = location.protocol === "https:" ? "wss" : "ws";
  const ws = new WebSocket(`${proto}://${location.host}/ws/progress`);
  ws.onmessage = (event) => {
    try {
      const msg = JSON.parse(event.data);
      if (window.Migration && typeof window.Migration.onProgress === "function") {
        window.Migration.onProgress(msg);
      }
    } catch (_) {
      // ignore
    }
  };
}

document.addEventListener("DOMContentLoaded", () => {
  initTabs();
  initCredentialsFile();
  initWS();
});
