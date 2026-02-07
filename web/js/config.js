const Config = {
  getFirebase() {
    const sourceType = document.querySelector("input[name=sourceType]:checked")?.value || "firestore";
    return {
      sourceType,
      projectId: $("projectId").value.trim(),
      credentialsJSON: $("credentialsJson").value.trim(),
    };
  },

  getPostgres() {
    return {
      url: $("pgUrl").value.trim(),
      host: $("pgHost").value.trim(),
      port: Number($("pgPort").value || 5432),
      database: $("pgDatabase").value.trim(),
      user: $("pgUser").value.trim(),
      password: $("pgPassword").value,
      sslMode: $("pgSslmode").value.trim(),
    };
  },

  getOptions() {
    return {
      batchSize: Number($("batchSize").value || 500),
      workers: Number($("parallelism").value || 1),
      errorStrategy: $("errorStrategy").value || "continue",
      dryRun: $("dryRun")?.checked || false,
    };
  },

  getMappings() {
    const cards = Array.from(document.querySelectorAll(".mapping-card"));
    const mappings = [];
    for (const card of cards) {
      const name = card.querySelector("[data-field=name]")?.value.trim();
      const sourceCollection = card.querySelector("[data-field=source]")?.value.trim();
      const targetTable = card.querySelector("[data-field=target]")?.value.trim();
      const createTable = card.querySelector("[data-field=create]")?.checked || false;
      const columnsText = card.querySelector("[data-field=columns]")?.value.trim();
      if (!name || !sourceCollection || !targetTable) {
        continue;
      }
      let columns = [];
      if (columnsText) {
        try {
          columns = JSON.parse(columnsText);
        } catch (e) {
          throw new Error(`columns JSON parse failed for ${name}`);
        }
      }
      mappings.push({ name, sourceCollection, targetTable, createTable, columns });
    }
    return mappings;
  },

  buildRequest() {
    return {
      firebase: this.getFirebase(),
      destination: this.getPostgres(),
      mappings: this.getMappings(),
      options: this.getOptions(),
    };
  },
  
  validatePostgres() {
    const pg = this.getPostgres();
    if (pg.url) {
      return null;
    }
    if (!pg.host || !pg.database || !pg.user) {
      return "DATABASE_URL か、Host/DB/User を入力してください";
    }
    return null;
  },

  async testFirebase() {
    $("firebaseTestResult").textContent = "接続テスト中...";
    const res = await fetch("/api/test-firebase", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(this.getFirebase()),
    });
    const data = await res.json().catch(() => ({}));
    $("firebaseTestResult").textContent = data.success ? "接続成功" : `失敗: ${data.error || "unknown"}`;
  },

  async testPostgres() {
    $("postgresTestResult").textContent = "接続テスト中...";
    const errMsg = this.validatePostgres();
    if (errMsg) {
      $("postgresTestResult").textContent = errMsg;
      return;
    }
    const res = await fetch("/api/test-postgres", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(this.getPostgres()),
    });
    const data = await res.json().catch(() => ({}));
    $("postgresTestResult").textContent = data.success ? "接続成功" : `失敗: ${data.error || "unknown"}`;
  },

  addMapping() {
    const container = $("mappings-container");
    const card = document.createElement("div");
    card.className = "section mapping-card";
    card.innerHTML = `
      <div class="form-row">
        <div class="form-group">
          <label>マッピング名</label>
          <input type="text" data-field="name" placeholder="users" />
        </div>
        <div class="form-group">
          <label>コレクション</label>
          <input type="text" data-field="source" list="collectionList" placeholder="users" />
        </div>
        <div class="form-group">
          <label>ターゲットテーブル</label>
          <input type="text" data-field="target" placeholder="public.users" />
        </div>
      </div>
      <div class="form-row">
        <label class="checkbox"><input type="checkbox" data-field="create" /> テーブルを作成</label>
      </div>
      <div class="form-group">
        <label>カラム定義 (JSON 配列)</label>
        <textarea rows="4" data-field="columns" placeholder='[
  {"source":"_id","target":"id","type":"TEXT","primaryKey":true,"nullable":false}
]'></textarea>
      </div>
      <div class="btn-group">
        <button class="btn btn-secondary" type="button" data-action="remove">削除</button>
      </div>
    `;
    card.querySelector("[data-action=remove]").addEventListener("click", () => {
      card.remove();
      this.refreshMappingSelect();
    });
    container.appendChild(card);
    this.refreshMappingSelect();
  },

  async fetchCollections() {
    try {
      const res = await fetch("/api/collections/tree", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ firebase: this.getFirebase(), sampleSize: 20 }),
      });
      if (!res.ok) {
        const text = await res.text();
        alert(`取得失敗: ${text || res.status}`);
        return;
      }
      const data = await res.json();
      const list = $("collectionList");
      list.innerHTML = "";
      (data.collections || []).forEach((name) => {
        const opt = document.createElement("option");
        opt.value = name;
        list.appendChild(opt);
      });

      this.ensureMappingsFromTree(data);
    } catch (e) {
      alert(`取得失敗: ${e.message || e}`);
    }
  },

  async generateMappingsAI() {
    try {
      const status = $("aiMappingStatus");
      const btn = $("btnAIMapping");
      if (status) {
        status.textContent = "AIでマッピング生成中...";
        status.classList.add("active");
      }
      if (btn) {
        btn.disabled = true;
        btn.textContent = "生成中...";
      }
      const res = await fetch("/api/mappings/ai", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ firebase: this.getFirebase(), sampleSize: 5, maxCollections: 20 }),
      });
      if (!res.ok) {
        const text = await res.text();
        if (status) {
          status.textContent = `生成失敗: ${text || res.status}`;
          status.classList.remove("active");
        }
        alert(`生成失敗: ${text || res.status}`);
        if (btn) {
          btn.disabled = false;
          btn.textContent = "AIでマッピング生成";
        }
        return;
      }
      const data = await res.json();
      const mappings = data.mappings || [];
      if (mappings.length === 0) {
        if (status) {
          status.textContent = "マッピングが生成されませんでした";
          status.classList.remove("active");
        }
        alert("マッピングが生成されませんでした");
        if (btn) {
          btn.disabled = false;
          btn.textContent = "AIでマッピング生成";
        }
        return;
      }
      this.applyMappings(mappings);
      if (status) {
        status.textContent = `AI生成完了: ${mappings.length} 件`;
        status.classList.remove("active");
      }
      if (btn) {
        btn.disabled = false;
        btn.textContent = "AIでマッピング生成";
      }
    } catch (e) {
      const status = $("aiMappingStatus");
      if (status) {
        status.textContent = `生成失敗: ${e.message || e}`;
        status.classList.remove("active");
      }
      alert(`生成失敗: ${e.message || e}`);
      const btn = $("btnAIMapping");
      if (btn) {
        btn.disabled = false;
        btn.textContent = "AIでマッピング生成";
      }
    }
  },

  applyMappings(mappings) {
    const container = $("mappings-container");
    container.innerHTML = "";
    mappings.forEach((m) => {
      this.addMapping();
      const card = container.lastElementChild;
      card.querySelector("[data-field=name]").value = m.name || "";
      card.querySelector("[data-field=source]").value = m.sourceCollection || "";
      card.querySelector("[data-field=target]").value = m.targetTable || "";
      card.querySelector("[data-field=create]").checked = m.createTable !== false;
      card.querySelector("[data-field=columns]").value = JSON.stringify(m.columns || [], null, 2);
    });
    this.refreshMappingSelect();
  },

  ensureMappingsFromTree(data) {
    const container = $("mappings-container");
    const hasMappings = container.querySelectorAll(".mapping-card").length > 0;
    if (!hasMappings) {
      (data.collections || []).forEach((name) => {
        this.addMappingWithDefaults({
          name,
          sourceCollection: name,
          targetTable: `public.${name}`,
        });
      });
      const subs = data.subcollections || {};
      Object.keys(subs).forEach((parent) => {
        subs[parent].forEach((sub) => {
          const target = `public.${parent}_${sub}`;
          this.addMappingWithDefaults({
            name: `${parent}_${sub}`,
            sourceCollection: `${parent}/*/${sub}`,
            targetTable: target,
          });
        });
      });
    }

    const cards = Array.from(document.querySelectorAll(".mapping-card"));
    cards.forEach((card) => {
      const source = card.querySelector("[data-field=source]")?.value.trim();
      const columnsText = card.querySelector("[data-field=columns]")?.value.trim();
      if (source && !columnsText) {
        // AIマッピングを基本とするため自動推論は行わない
      }
    });
  },

  addMappingWithDefaults({ name, sourceCollection, targetTable }) {
    this.addMapping();
    const container = $("mappings-container");
    const card = container.lastElementChild;
    card.querySelector("[data-field=name]").value = name || "";
    card.querySelector("[data-field=source]").value = sourceCollection || "";
    card.querySelector("[data-field=target]").value = targetTable || "";
    card.querySelector("[data-field=create]").checked = true;
    this.refreshMappingSelect();
  },

  // カラム自動取得は廃止（AIマッピングを使用）

  refreshMappingSelect() {
    const select = $("schemaMappingSelect");
    if (!select) return;
    const mappings = this.getMappings();
    select.innerHTML = "";
    mappings.forEach((m) => {
      const opt = document.createElement("option");
      opt.value = m.name;
      opt.textContent = `${m.name} (${m.targetTable})`;
      select.appendChild(opt);
    });
  },

  save() {
    try {
      const payload = this.buildRequest();
      const name = $("configName").value.trim();
      if (!name) {
        alert("設定名を入力してください");
        return;
      }
      localStorage.setItem(`f2p:${name}`, JSON.stringify(payload));
      alert("保存しました");
      this.refreshMappingSelect();
    } catch (e) {
      alert(e.message || "保存に失敗しました");
    }
  },

  showLoadDialog() {
    const list = $("configList");
    list.innerHTML = "";
    const keys = Object.keys(localStorage).filter((k) => k.startsWith("f2p:"));
    if (keys.length === 0) {
      list.textContent = "保存済み設定はありません";
    } else {
      keys.forEach((k) => {
        const name = k.replace("f2p:", "");
        const btn = document.createElement("button");
        btn.className = "btn btn-secondary";
        btn.textContent = name;
        btn.addEventListener("click", () => {
          const data = JSON.parse(localStorage.getItem(k));
          this.applyConfig(data);
          this.closeLoadDialog();
        });
        list.appendChild(btn);
      });
    }
    $("loadDialog").style.display = "flex";
  },

  closeLoadDialog() {
    $("loadDialog").style.display = "none";
  },

  applyConfig(data) {
    if (!data) return;
    const fb = data.firebase || {};
    document.querySelectorAll("input[name=sourceType]").forEach((r) => {
      r.checked = r.value === (fb.sourceType || "firestore");
    });
    $("projectId").value = fb.projectId || "";
    $("credentialsJson").value = fb.credentialsJSON || "";

    const pg = data.destination || {};
    $("pgUrl").value = pg.url || "";
    $("pgHost").value = pg.host || "";
    $("pgPort").value = pg.port || 5432;
    $("pgDatabase").value = pg.database || "";
    $("pgUser").value = pg.user || "";
    $("pgPassword").value = pg.password || "";
    $("pgSslmode").value = pg.sslMode || "prefer";

    const opt = data.options || {};
    $("batchSize").value = opt.batchSize || 500;
    $("parallelism").value = opt.workers || 1;
    $("errorStrategy").value = opt.errorStrategy || "continue";

    const container = $("mappings-container");
    container.innerHTML = "";
    (data.mappings || []).forEach((m) => {
      this.addMapping();
      const card = container.lastElementChild;
      card.querySelector("[data-field=name]").value = m.name || "";
      card.querySelector("[data-field=source]").value = m.sourceCollection || "";
      card.querySelector("[data-field=target]").value = m.targetTable || "";
      card.querySelector("[data-field=create]").checked = !!m.createTable;
      card.querySelector("[data-field=columns]").value = JSON.stringify(m.columns || [], null, 2);
    });

    this.refreshMappingSelect();
  },
};

window.Config = Config;
