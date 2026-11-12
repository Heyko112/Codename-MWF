// Typen für mwf-widget.js. Werte je Spaltentyp:
// text/longtext: string · int: number · decimal: string ("1234.50") · date: "YYYY-MM-DD"
// datetime: "YYYY-MM-DDTHH:MM:SS" · bool: boolean · leer: null

type MwfValue = string | number | boolean | null;

interface MwfContext {
  /** roles: wirksame Rollen des Benutzers (auch über enthaltene Rollen), ohne versteckte */
  user: { username: string; displayName: string; roles: string[] };
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
  /** true, wenn der Benutzer nur einen Teil der Zeilen lesen darf (Zeilenfilter) */
  filtered: boolean;
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
  /** schränkt weiter ein (alle Bedingungen zusammen). Die Zeilenfilter der Rechte gelten immer. */
  where?: MwfCondition[];
  /** Spaltenschlüssel, "-" davor = absteigend, z. B. "-datum". Standard: neueste zuerst ("-id") */
  order?: string;
}

interface MwfCondition {
  /** Spaltenschlüssel oder "createdBy" (angelegt von, Wert "{ich}" oder Login-Name) */
  col: string;
  op: "eq" | "ne" | "lt" | "le" | "gt" | "ge" | "contains" | "empty" | "notempty";
  /** wie bei create; dazu "{ich}" (Login-Name) und "{heute}" (Datum). Bei empty/notempty weglassen. */
  value?: MwfValue;
}

interface MwfUser {
  username: string;
  displayName: string;
  /** wirksame Rollen, ohne versteckte */
  roles: string[];
}

interface MwfUsersOptions {
  /** nur Mitglieder dieser Rolle (auch über enthaltene Rollen), Groß-/Kleinschreibung egal */
  role?: string;
  /** sucht in Login-Name und Anzeigename */
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
  /** "kachel" auf der Startseite, sonst "vollbild". Steht auch als data-ansicht an <html>. */
  view: "kachel" | "vollbild";
  /** gewähltes Design, "" = wie das Betriebssystem. Steht auch als data-theme an <html>. */
  theme: "light" | "dark" | "";
  MwfError: { new (code: string, message: string, fields?: Record<string, string>): MwfError };
  context(): Promise<MwfContext>;
  tables(): Promise<MwfTable[]>;
  /** Einstellungen aus manifest.json "settings", Werte vom Verwalter, nach Schlüssel */
  settings(): Promise<Record<string, MwfValue>>;
  /** aktive Benutzer ohne versteckte und Service-User, nach Login-Name. Braucht im Manifest "users": "read". */
  users(options?: MwfUsersOptions): Promise<MwfUser[]>;
  /** alle nicht versteckten Rollen, nach Name. Braucht im Manifest "users": "read". */
  roles(): Promise<{ name: string; description: string }[]>;
  /** neueste zuerst, wenn options.order fehlt. Nur Zeilen, die der Benutzer lesen darf. */
  list(table: string, options?: MwfListOptions): Promise<{ total: number; records: MwfRecord[] }>;
  get(table: string, id: number): Promise<MwfRecord>;
  create(table: string, values: Record<string, MwfValue>): Promise<MwfRecord>;
  /** ändert nur die übergebenen Spalten */
  update(table: string, id: number, values: Record<string, MwfValue>): Promise<MwfRecord>;
  remove(table: string, id: number): Promise<{ deleted: number }>;
}

declare const mwf: Mwf;
