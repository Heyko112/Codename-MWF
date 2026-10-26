// Typen für mwf-widget.js. Werte je Spaltentyp:
// text/longtext: string · int: number · decimal: string ("1234.50") · date: "YYYY-MM-DD"
// datetime: "YYYY-MM-DDTHH:MM:SS" · bool: boolean · leer: null

type MwfValue = string | number | boolean | null;

interface MwfContext {
  user: { username: string; displayName: string };
  widget: { id: string; name: string; version: string };
}

interface MwfPerms { read: boolean; create: boolean; update: boolean; delete: boolean }

interface MwfColumn {
  key: string;
  name: string;
  type: "text" | "longtext" | "int" | "decimal" | "date" | "datetime" | "bool";
  size: number;
  required: boolean;
}

interface MwfTable {
  alias: string;
  description: string;
  /** false, solange ein Verwalter noch keine Tabelle zugeordnet hat */
  mapped: boolean;
  /** wirksam: Recht des Benutzers ∩ Freigabe des Widgets */
  perms: MwfPerms;
  /** nur, wenn zugeordnet und irgendein Recht besteht */
  columns?: MwfColumn[];
}

interface MwfRecord {
  id: number;
  /** UTC, z. B. "2026-10-24T13:05:00Z" */
  createdAt: string;
  updatedAt: string | null;
  values: Record<string, MwfValue>;
}

interface MwfListOptions {
  /** 1–200, Standard 50 */
  limit?: number;
  offset?: number;
  /** sucht in Text-Spalten und nach ID */
  search?: string;
}

interface MwfError extends Error {
  name: "MwfError";
  /** not_found | forbidden | invalid | not_mapped | bad_request | session | network | timeout | busy | server | no_host */
  code: string;
  /** bei invalid: Fehlermeldung je Spaltenschlüssel */
  fields: Record<string, string>;
}

interface Mwf {
  MwfError: { new (code: string, message: string, fields?: Record<string, string>): MwfError };
  context(): Promise<MwfContext>;
  tables(): Promise<MwfTable[]>;
  /** neueste zuerst */
  list(table: string, options?: MwfListOptions): Promise<{ total: number; records: MwfRecord[] }>;
  get(table: string, id: number): Promise<MwfRecord>;
  create(table: string, values: Record<string, MwfValue>): Promise<MwfRecord>;
  /** ändert nur die übergebenen Spalten */
  update(table: string, id: number, values: Record<string, MwfValue>): Promise<MwfRecord>;
  remove(table: string, id: number): Promise<{ deleted: number }>;
}

declare const mwf: Mwf;
