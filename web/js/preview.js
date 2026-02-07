const Preview = {
  records: [],
  index: 0,

  async fetch() {
    const collection = $("previewCollection").value.trim();
    const limit = Number($("previewLimit").value || 5);
    if (!collection) {
      alert("コレクション名を入力してください");
      return;
    }
    const payload = {
      firebase: Config.getFirebase(),
      collection,
      limit,
    };
    const res = await fetch("/api/preview", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!res.ok) {
      alert(`取得失敗: ${await res.text()}`);
      return;
    }
    const data = await res.json();
    this.records = data.records || [];
    this.index = 0;
    this.render();
  },

  render() {
    const container = $("preview-container");
    if (this.records.length === 0) {
      container.innerHTML = "<p class=\"placeholder\">データがありません</p>";
      $("preview-nav").style.display = "none";
      return;
    }
    const current = this.records[this.index] || {};
    container.innerHTML = `<pre class=\"code-block\">${JSON.stringify(current, null, 2)}</pre>`;
    $("preview-nav").style.display = "flex";
    $("previewIndex").textContent = `${this.index + 1}/${this.records.length}`;
  },

  prev() {
    if (this.records.length === 0) return;
    this.index = (this.index - 1 + this.records.length) % this.records.length;
    this.render();
  },

  next() {
    if (this.records.length === 0) return;
    this.index = (this.index + 1) % this.records.length;
    this.render();
  },
};

window.Preview = Preview;
