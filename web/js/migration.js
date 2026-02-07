const Migration = {
  async start() {
    try {
      const payload = Config.buildRequest();
      const pgErr = Config.validatePostgres();
      if (pgErr) {
        alert(pgErr);
        return;
      }
      if (!payload.mappings || payload.mappings.length === 0) {
        alert("マッピングを追加してください");
        return;
      }
      const missingCols = payload.mappings.find((m) => !m.columns || m.columns.length === 0);
      if (missingCols) {
        alert(`カラム定義が未設定です: ${missingCols.name}`);
        return;
      }
      const res = await fetch("/api/migrate", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });
      if (!res.ok) {
        const msg = await res.text();
        alert(`開始失敗: ${msg}`);
        return;
      }
      $("btnStart").disabled = true;
      $("btnStop").disabled = false;
      this.log("migration started");
    } catch (e) {
      alert(e.message || "開始に失敗しました");
    }
  },

  async stop() {
    await fetch("/api/migrate/cancel", { method: "POST" });
    $("btnStart").disabled = false;
    $("btnStop").disabled = true;
    this.log("cancel requested");
  },

  clearLogs() {
    $("log-container").textContent = "";
  },

  log(msg) {
    const area = $("log-container");
    const line = document.createElement("div");
    line.textContent = msg;
    area.appendChild(line);
    area.scrollTop = area.scrollHeight;
  },

  onProgress(status) {
    if (!status) return;
    const container = $("progress-container");
    container.innerHTML = "";
    const summary = document.createElement("div");
    summary.innerHTML = `
      <div>全体: ${status.overallPercent || 0}%</div>
      <div>経過: ${status.elapsedSeconds || 0}s</div>
      <div>フェーズ: ${status.currentPhase || "-"}</div>
    `;
    container.appendChild(summary);

    const rows = status.tables || [];
    rows.forEach((t) => {
      const row = document.createElement("div");
      row.className = "table-row";
      row.innerHTML = `
        <span>${t.name || "-"}</span>
        <span>${t.percent || 0}% (${t.phase || "-"})</span>
        <span>read ${t.read || 0} / written ${t.written || 0} / errors ${t.errors || 0}</span>
      `;
      container.appendChild(row);
    });

    if (status.logMessage) {
      this.log(status.logMessage);
    }

    if (!status.running) {
      $("btnStart").disabled = false;
      $("btnStop").disabled = true;
    }
  },
};

window.Migration = Migration;
