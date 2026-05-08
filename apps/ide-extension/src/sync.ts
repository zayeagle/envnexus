import * as vscode from "vscode";
import { inflateRaw } from "zlib";
import { promisify } from "util";
import fetch from "node-fetch";
import { getApiBase, getValidAccessToken } from "./auth";

const inflateRawAsync = promisify(inflateRaw);

type ApiSuccess<T> = { data: T; error: null; request_id?: string };
type ApiErrorBody = { data: null; error: { code: string; message: string }; request_id?: string };

function isApiError(res: ApiSuccess<unknown> | ApiErrorBody): res is ApiErrorBody {
  return res.error != null;
}

interface ManifestData {
  items: ManifestItem[];
}

interface ManifestItem {
  id: string;
  type: string;
  name: string;
  payload: string;
}

interface ZipEntry {
  fileName: string;
  data: Uint8Array;
}

/**
 * Parses a zip buffer using the Central Directory and extracts all file entries.
 * Supports Store (method 0) and Deflate (method 8) only, which covers all
 * standard zip files produced by common tools.
 */
async function extractZipEntries(buf: Buffer): Promise<ZipEntry[]> {
  const eocdSig = 0x06054b50;
  let eocdOffset = -1;
  for (let i = buf.length - 22; i >= 0 && i >= buf.length - 65535 - 22; i--) {
    if (buf.readUInt32LE(i) === eocdSig) {
      eocdOffset = i;
      break;
    }
  }
  if (eocdOffset < 0) {
    throw new Error("Invalid zip: EOCD not found");
  }

  const cdOffset = buf.readUInt32LE(eocdOffset + 16);
  const cdEntries = buf.readUInt16LE(eocdOffset + 10);
  const entries: ZipEntry[] = [];
  let pos = cdOffset;
  const cdSig = 0x02014b50;

  for (let i = 0; i < cdEntries; i++) {
    if (buf.readUInt32LE(pos) !== cdSig) {
      throw new Error("Invalid zip: bad central directory entry");
    }
    const method = buf.readUInt16LE(pos + 10);
    const compSize = buf.readUInt32LE(pos + 20);
    const uncompSize = buf.readUInt32LE(pos + 24);
    const nameLen = buf.readUInt16LE(pos + 28);
    const extraLen = buf.readUInt16LE(pos + 30);
    const commentLen = buf.readUInt16LE(pos + 32);
    const localHeaderOffset = buf.readUInt32LE(pos + 42);
    const fileName = buf.toString("utf8", pos + 46, pos + 46 + nameLen);
    pos += 46 + nameLen + extraLen + commentLen;

    if (fileName.endsWith("/")) {
      continue;
    }

    const localSig = 0x04034b50;
    if (buf.readUInt32LE(localHeaderOffset) !== localSig) {
      throw new Error(`Invalid zip: bad local header for ${fileName}`);
    }
    const localNameLen = buf.readUInt16LE(localHeaderOffset + 26);
    const localExtraLen = buf.readUInt16LE(localHeaderOffset + 28);
    const dataStart = localHeaderOffset + 30 + localNameLen + localExtraLen;
    const compressedData = buf.subarray(dataStart, dataStart + compSize);

    let data: Uint8Array;
    if (method === 0) {
      data = new Uint8Array(compressedData);
    } else if (method === 8) {
      data = await inflateRawAsync(compressedData);
    } else {
      throw new Error(`Unsupported zip compression method ${method} for ${fileName}`);
    }

    if (data.length !== uncompSize && method !== 0) {
      throw new Error(`Size mismatch for ${fileName}: expected ${uncompSize}, got ${data.length}`);
    }
    entries.push({ fileName, data });
  }
  return entries;
}

const ENC = new TextEncoder();

function sanitizePathSegment(name: string): string {
  return name.replace(/[/\\?*<>|":\x00-\x1f]+/g, "-").replace(/^\.+/, "") || "item";
}

/** Creates each segment under `root` if it does not exist. Returns the URI for the full path. */
async function ensureDirPath(root: vscode.Uri, parts: string[]): Promise<vscode.Uri> {
  let cur = root;
  for (const p of parts) {
    cur = vscode.Uri.joinPath(cur, p);
    try {
      await vscode.workspace.fs.stat(cur);
    } catch {
      await vscode.workspace.fs.createDirectory(cur);
    }
  }
  return cur;
}

function mergeMcpDocument(
  existing: Record<string, unknown> | undefined,
  itemName: string,
  payloadStr: string
): Record<string, unknown> {
  const parsed: unknown = JSON.parse(payloadStr);
  const base: Record<string, unknown> = existing ? { ...existing } : {};
  const mcpServers: Record<string, unknown> = {
    ...(typeof base.mcpServers === "object" && base.mcpServers !== null && !Array.isArray(base.mcpServers)
      ? (base.mcpServers as Record<string, unknown>)
      : {})
  };
  if (
    typeof parsed === "object" &&
    parsed !== null &&
    "mcpServers" in parsed &&
    typeof (parsed as { mcpServers: unknown }).mcpServers === "object" &&
    (parsed as { mcpServers: Record<string, unknown> }).mcpServers !== null
  ) {
    const add = (parsed as { mcpServers: Record<string, unknown> }).mcpServers;
    Object.assign(mcpServers, add);
  } else if (typeof parsed === "object" && parsed !== null) {
    mcpServers[sanitizePathSegment(itemName)] = parsed;
  } else {
    throw new Error("MCP payload must be a JSON object or { mcpServers: { ... } }");
  }
  return { ...base, mcpServers };
}

async function readJsonObjectFile(uri: vscode.Uri): Promise<Record<string, unknown> | undefined> {
  try {
    const raw = await vscode.workspace.fs.readFile(uri);
    const text = new TextDecoder("utf-8").decode(raw);
    const parsed: unknown = JSON.parse(text);
    if (typeof parsed === "object" && parsed !== null && !Array.isArray(parsed)) {
      return parsed as Record<string, unknown>;
    }
  } catch {
    // missing or invalid — treat as empty
  }
  return undefined;
}

async function applyMcpItem(folder: vscode.Uri, item: ManifestItem): Promise<void> {
  const cursor = vscode.Uri.joinPath(folder, ".cursor");
  const mcpPath = vscode.Uri.joinPath(cursor, "mcp.json");
  const existing = await readJsonObjectFile(mcpPath);
  const merged = mergeMcpDocument(existing, item.name, item.payload);
  await ensureDirPath(folder, [".cursor"]);
  await vscode.workspace.fs.writeFile(mcpPath, ENC.encode(`${JSON.stringify(merged, null, 2)}\n`));
}

function parseObjectPayload(payload: string): { object_key?: string; download_url?: string } {
  try {
    return JSON.parse(payload) as { object_key?: string; download_url?: string };
  } catch {
    return {};
  }
}

async function downloadZipBuffer(url: string): Promise<Buffer> {
  const res = await fetch(url);
  if (!res.ok) {
    throw new Error(`Download failed (${res.status}): ${url}`);
  }
  return Buffer.from(await res.arrayBuffer());
}

async function extractZipToDir(zipBuf: Buffer, targetDir: vscode.Uri): Promise<void> {
  const entries = await extractZipEntries(zipBuf);
  for (const entry of entries) {
    const parts = entry.fileName.split("/").filter(Boolean);
    if (parts.length === 0) { continue; }
    if (parts.length > 1) {
      await ensureDirPath(targetDir, parts.slice(0, -1));
    }
    const fileUri = vscode.Uri.joinPath(targetDir, ...parts);
    await vscode.workspace.fs.writeFile(fileUri, entry.data);
  }
}

async function applySkillItem(folder: vscode.Uri, item: ManifestItem): Promise<void> {
  const p = parseObjectPayload(item.payload);
  if (p.download_url) {
    const base = sanitizePathSegment(item.name);
    const dir = await ensureDirPath(folder, [".cursor", "skills", base]);
    const zipBuf = await downloadZipBuffer(p.download_url);
    await extractZipToDir(zipBuf, dir);
  } else {
    const base = sanitizePathSegment(item.name);
    const dir = await ensureDirPath(folder, [".cursor", "skills", base]);
    const file = vscode.Uri.joinPath(dir, "SKILL.md");
    await vscode.workspace.fs.writeFile(file, ENC.encode(item.payload));
  }
}

async function applyRuleItem(folder: vscode.Uri, item: ManifestItem): Promise<void> {
  const p = parseObjectPayload(item.payload);
  if (p.download_url) {
    const dir = await ensureDirPath(folder, [".cursor", "rules"]);
    const zipBuf = await downloadZipBuffer(p.download_url);
    await extractZipToDir(zipBuf, dir);
  } else {
    const base = sanitizePathSegment(item.name);
    const nameWithExt = base.toLowerCase().endsWith(".mdc") ? base : `${base}.mdc`;
    await ensureDirPath(folder, [".cursor", "rules"]);
    const file = vscode.Uri.joinPath(folder, ".cursor", "rules", nameWithExt);
    await vscode.workspace.fs.writeFile(file, ENC.encode(item.payload));
  }
}

async function fetchIdeSyncManifest(accessToken: string): Promise<{ items: ManifestItem[] }> {
  const res = await fetch(`${getApiBase()}/ide-sync/manifest`, {
    method: "GET",
    headers: {
      Accept: "application/json",
      Authorization: `Bearer ${accessToken}`
    }
  });
  const text = await res.text();
  let parsed: ApiSuccess<ManifestData> | ApiErrorBody;
  try {
    parsed = JSON.parse(text) as ApiSuccess<ManifestData> | ApiErrorBody;
  } catch {
    throw new Error(`Invalid JSON from ide-sync manifest (${res.status}): ${text.slice(0, 200)}`);
  }
  if (isApiError(parsed)) {
    throw new Error(parsed.error.message);
  }
  if (!res.ok) {
    throw new Error(`Request failed (${res.status})`);
  }
  const data = parsed.data;
  if (!data || !Array.isArray(data.items)) {
    throw new Error("Manifest response missing items");
  }
  return data;
}

/**
 * Pulls subscribed marketplace items from the platform API and materializes them under `.cursor/`
 * in each workspace folder.
 */
export async function sync(context: vscode.ExtensionContext): Promise<void> {
  const token = await getValidAccessToken(context);
  if (!token) {
    void vscode.window.showErrorMessage("Please login first");
    return;
  }

  const roots = vscode.workspace.workspaceFolders;
  if (!roots?.length) {
    void vscode.window.showErrorMessage("EnvNexus: open a folder or workspace to sync.");
    return;
  }

  let data: { items: ManifestItem[] };
  try {
    data = await fetchIdeSyncManifest(token);
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e);
    void vscode.window.showErrorMessage(`EnvNexus sync failed: ${msg}`);
    return;
  }

  for (const wf of roots) {
    for (const item of data.items) {
      try {
        if (item.type === "mcp") {
          await applyMcpItem(wf.uri, item);
        } else if (item.type === "skill") {
          await applySkillItem(wf.uri, item);
        } else if (item.type === "rule") {
          await applyRuleItem(wf.uri, item);
        }
        // plugin, subagent: not synced here
      } catch (e) {
        const msg = e instanceof Error ? e.message : String(e);
        void vscode.window.showErrorMessage(
          `EnvNexus: could not write ${item.type} “${item.name}”: ${msg}`
        );
        return;
      }
    }
  }

  void vscode.window.showInformationMessage("EnvNexus: sync completed.");
}
