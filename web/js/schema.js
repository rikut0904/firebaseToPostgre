const Schema = {
  generate() {
    const name = $("schemaMappingSelect").value;
    const mappings = Config.getMappings();
    const mapping = mappings.find((m) => m.name === name) || mappings[0];
    if (!mapping) {
      $("ddlOutput").textContent = "マッピングがありません";
      return;
    }
    const ddl = this.buildDDL(mapping);
    $("ddlOutput").textContent = ddl;
  },

  buildDDL(mapping) {
    const cols = (mapping.columns || []).map((c) => {
      const nullable = c.nullable ? "" : " NOT NULL";
      return `  "${c.target}" ${c.type}${nullable}`;
    });
    const pks = (mapping.columns || []).filter((c) => c.primaryKey).map((c) => `"${c.target}"`);
    if (pks.length > 0) {
      cols.push(`  PRIMARY KEY (${pks.join(", ")})`);
    }
    return `CREATE TABLE IF NOT EXISTS ${mapping.targetTable} (\n${cols.join(",\n")}\n);`;
  },

  copyDDL() {
    const text = $("ddlOutput").textContent;
    if (!text) return;
    navigator.clipboard.writeText(text);
  },

  executeDDL() {
    $("schemaExecResult").textContent = "DDL実行は未対応です";
  },
};

window.Schema = Schema;
