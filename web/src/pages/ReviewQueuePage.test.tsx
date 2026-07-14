import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ReviewQueuePage } from "./ReviewQueuePage";

const listReviewQueue = vi.hoisted(() => vi.fn());
vi.mock("../lib/api", () => ({ listReviewQueue }));

describe("ReviewQueuePage", () => {
  beforeEach(() => {
    listReviewQueue.mockResolvedValue([{
      document_id: "doc", document_title: "高分手稿", page_id: "page", page_no: 2,
      page_status: "recognized", thumbnail_url: "", confidence: 0.95, recognition_count: 1,
      open_uncertain_count: 0, last_provider: "provider", last_model: "model", updated_at: "2026-01-01T00:00:00Z",
      low_confidence_segments: [{ text: "低词", start: 3, end: 5, confidence: 0.12, level: "word", source: "words.confidence" }],
    }]);
  });

  it("shows fine-grained low-confidence segments even when page confidence is high", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<QueryClientProvider client={client}><MemoryRouter><ReviewQueuePage /></MemoryRouter></QueryClientProvider>);
    expect(await screen.findByText("低词")).toBeInTheDocument();
    expect(screen.getByText("12%")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /低词/ })).toHaveAttribute("href", "/review/doc/page?focus_start=3&focus_end=5");
  });
});
