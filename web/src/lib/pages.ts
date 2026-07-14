import type { PageDetail } from "@/lib/api";

export function parsePageSpec(spec: string, pages: PageDetail[]) {
  const byNumber = new Map(pages.map((page) => [page.page_no, page.page_id]));
  const selected = new Set<number>();
  const invalid: string[] = [];

  for (const rawPart of spec.split(/[，,]/)) {
    const part = rawPart.trim();
    if (!part) continue;
    const range = part.match(/^(\d+)\s*-\s*(\d+)$/);
    if (range) {
      const start = Number(range[1]);
      const end = Number(range[2]);
      if (start > end || end - start > 10000) {
        invalid.push(part);
        continue;
      }
      for (let pageNo = start; pageNo <= end; pageNo += 1) selected.add(pageNo);
      continue;
    }
    if (/^\d+$/.test(part)) {
      selected.add(Number(part));
      continue;
    }
    invalid.push(part);
  }

  const missing = [...selected].filter((pageNo) => !byNumber.has(pageNo));
  if (invalid.length) return { pageIDs: [] as string[], error: `无法识别页码：${invalid.join("、")}` };
  if (missing.length) return { pageIDs: [] as string[], error: `文档中不存在第 ${missing.join("、")} 页` };
  return {
    pageIDs: [...selected].sort((left, right) => left - right).map((pageNo) => byNumber.get(pageNo)!),
    error: selected.size ? "" : "请至少选择一页。",
  };
}
