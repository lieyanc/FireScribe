import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { CrossCheckCard } from "./cross-check-card";
import type { CrossCheck, PageDetail } from "@/lib/api";

const api = vi.hoisted(() => ({
  listRecognizerProfiles: vi.fn(),
  listProviderAdapters: vi.fn(),
  listPromptVersions: vi.fn(),
  listCrossChecks: vi.fn(),
  getCrossCheck: vi.fn(),
  getJob: vi.fn(),
  cancelJob: vi.fn(),
  startCrossCheck: vi.fn(),
  adoptCrossCheck: vi.fn(),
}));
vi.mock("@/lib/api", () => api);

function makePage(no: number): PageDetail {
  return {
    page_id: `p${no}`, document_id: "doc-1", page_no: no, page_status: "recognized",
    width: 0, height: 0, image_asset_id: "", thumb_asset_id: "", recognition_count: 1,
    best_confidence: null, last_provider: "", last_model: "", last_recognized_at: "",
    has_candidate: false, has_manual: false, has_final: false, updated_at: "",
    image_url: "", thumbnail_url: "",
  };
}

const baseCheck: CrossCheck = {
  id: "cc-1", document_id: "doc-1", job_id: "job-1", name: "首次核验",
  page_ids: ["p1", "p2", "p3"], status: "succeeded", error: "",
  consensus_pages: 1, disagreement_pages: 1, failed_pages: 1,
  created_at: "2026-07-01T10:00:00Z", started_at: "2026-07-01T10:00:01Z", finished_at: "2026-07-01T10:01:00Z",
  variants: [
    {
      id: "v1", cross_check_id: "cc-1", name: "模型 A", recognizer_profile_id: "prof-1",
      image_source: "original", position: 0, status: "succeeded", error: "",
      created_at: "2026-07-01T10:00:00Z", started_at: "", finished_at: "",
    },
    {
      id: "v2", cross_check_id: "cc-1", name: "模型 B", recognizer_profile_id: "prof-2",
      image_source: "original", position: 1, status: "succeeded", error: "",
      created_at: "2026-07-01T10:00:00Z", started_at: "", finished_at: "",
    },
  ],
};

const detailCheck: CrossCheck = {
  ...baseCheck,
  pages: [
    {
      cross_check_id: "cc-1", page_id: "p1", page_no: 1, status: "consensus", agreement: 1,
      result_ids: ["r1", "r2"], conflicts: [], error: "",
    },
    {
      cross_check_id: "cc-1", page_id: "p2", page_no: 2, status: "disagreement", agreement: 0.92,
      result_ids: ["r3", "r4"], merged_version_id: "tv-1", annotation_id: "an-1", error: "",
      conflicts: [{ text: "他日相逢一杯酒", kind: "omitted", present_in: ["模型 A"], absent_from: ["模型 B"] }],
    },
    {
      cross_check_id: "cc-1", page_id: "p3", page_no: 3, status: "failed", agreement: null,
      result_ids: [], conflicts: [], error: "识别超时",
    },
  ],
};

// jsdom 缺少 radix Select 依赖的指针捕获与滚动 API。
beforeAll(() => {
  Object.assign(Element.prototype, {
    hasPointerCapture: () => false,
    setPointerCapture: () => {},
    releasePointerCapture: () => {},
    scrollIntoView: () => {},
  });
});

function renderCard() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <CrossCheckCard documentID="doc-1" pages={[makePage(1), makePage(2), makePage(3)]} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

async function selectOption(trigger: HTMLElement, optionName: string) {
  fireEvent.pointerDown(trigger, { button: 0, ctrlKey: false, pointerId: 1, pointerType: "mouse" });
  const option = await screen.findByRole("option", { name: optionName });
  // jsdom 下 pointerup 不会派发到 React,用 radix 的键盘选择路径(Enter)。
  fireEvent.keyDown(option, { key: "Enter" });
  await waitFor(() => expect(screen.queryByRole("option", { name: optionName })).not.toBeInTheDocument());
}

describe("CrossCheckCard", () => {
  afterEach(() => cleanup());

  beforeEach(() => {
    api.listRecognizerProfiles.mockResolvedValue([
      { id: "prof-1", name: "甲模型", is_default: true },
      { id: "prof-2", name: "乙模型", is_default: false },
    ]);
    api.listProviderAdapters.mockResolvedValue([]);
    api.listPromptVersions.mockResolvedValue([]);
    api.listCrossChecks.mockResolvedValue([baseCheck]);
    api.getCrossCheck.mockResolvedValue(detailCheck);
    api.getJob.mockResolvedValue({ id: "job-1", status: "succeeded", progress_current: 4, progress_total: 4, progress_message: "" });
  });

  it("renders history entries with counts and per-page verdicts", async () => {
    renderCard();
    expect(await screen.findByText("首次核验")).toBeInTheDocument();
    expect((await screen.findAllByText(/1 一致 \/ 1 分歧 \/ 1 失败/)).length).toBeGreaterThan(0);
    expect(await screen.findByText("一致度 92.0%")).toBeInTheDocument();
    expect(screen.getByText("合并稿未收录")).toBeInTheDocument();
    // 只有一致/分歧页待拍板;failed 页(第 3 页)不出现「待拍板」,错误信息可见。
    expect(screen.getAllByText("待拍板")).toHaveLength(2);
    expect(screen.getByText("识别超时")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "去审校" })).toHaveAttribute("href", "/review/doc-1/p2");
    expect(screen.getByRole("button", { name: "采纳" })).toBeInTheDocument();
  });

  it("validates the form before submitting and posts the cross check", async () => {
    api.listCrossChecks.mockResolvedValue([]);
    api.startCrossCheck.mockResolvedValue({ cross_check: baseCheck, job: { id: "job-1" } });
    renderCard();

    const createButton = await screen.findByRole("button", { name: "创建并运行" });
    expect(createButton).toBeDisabled();

    const sourceTriggers = await screen.findAllByLabelText("Profile / Adapter");
    await selectOption(sourceTriggers[0], "甲模型");
    await selectOption(sourceTriggers[1], "乙模型");
    expect(createButton).toBeEnabled();

    fireEvent.click(screen.getByRole("radio", { name: "选择页" }));
    const pageInput = screen.getByLabelText("页码");
    fireEvent.change(pageInput, { target: { value: "abc" } });
    expect(await screen.findByText("无法识别页码：abc")).toBeInTheDocument();
    expect(createButton).toBeDisabled();

    fireEvent.change(pageInput, { target: { value: "2" } });
    expect(createButton).toBeEnabled();
    fireEvent.click(createButton);

    await waitFor(() => expect(api.startCrossCheck).toHaveBeenCalledTimes(1));
    expect(api.startCrossCheck).toHaveBeenCalledWith("doc-1", {
      name: undefined,
      page_ids: ["p2"],
      merge_profile_id: undefined,
      variants: [
        { name: undefined, recognizer_profile_id: "prof-1", provider_adapter_id: undefined, prompt_version_id: undefined, image_source: "original" },
        { name: undefined, recognizer_profile_id: "prof-2", provider_adapter_id: undefined, prompt_version_id: undefined, image_source: "original" },
      ],
    });

    api.startCrossCheck.mockRejectedValue(new Error("已有进行中的交叉核验"));
    fireEvent.click(screen.getByRole("button", { name: "创建并运行" }));
    expect(await screen.findByText("已有进行中的交叉核验")).toBeInTheDocument();
  });

  it("adopts a single consensus page from the page table", async () => {
    api.adoptCrossCheck.mockResolvedValue({ adopted_page_ids: ["p1"], skipped: [], cross_check: detailCheck });
    renderCard();
    fireEvent.click(await screen.findByRole("button", { name: "采纳" }));
    await waitFor(() => expect(api.adoptCrossCheck).toHaveBeenCalledWith("cc-1", ["p1"]));
  });

  it("adopts all consensus pages after confirmation and lists skipped pages", async () => {
    api.adoptCrossCheck.mockResolvedValue({
      adopted_page_ids: ["p1"],
      skipped: [{ page_id: "p2", page_no: 2, reason: "该页已有人工版本，请在审校页确认" }],
      cross_check: detailCheck,
    });
    renderCard();
    fireEvent.click(await screen.findByRole("button", { name: "一键采纳全部一致页" }));
    fireEvent.click(await screen.findByRole("button", { name: /确认采纳/ }));
    await waitFor(() => expect(api.adoptCrossCheck).toHaveBeenCalledWith("cc-1", undefined));
    expect(await screen.findByText(/成功 1 页，跳过 1 页/)).toBeInTheDocument();
    expect(screen.getByText(/第 2 页：该页已有人工版本/)).toBeInTheDocument();
  });
});
