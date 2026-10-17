import type { ChangedFile } from "./data";
export function fileSignature(file: ChangedFile) {
  // Two independent 32-bit hashes plus the source length; only a progress hint.
  const text = JSON.stringify([
    file.path,
    file.previousPath,
    file.status,
    file.lines,
  ]);
  let a = 2166136261,
    b = 5381;
  for (let i = 0; i < text.length; i++) {
    a = Math.imul(a ^ text.charCodeAt(i), 16777619);
    b = Math.imul(b, 33) ^ text.charCodeAt(i);
  }
  return `${text.length}:${a >>> 0}:${b >>> 0}`;
}
