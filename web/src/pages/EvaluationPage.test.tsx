import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { EvaluationPage } from "./EvaluationPage";

const getEvaluationMetrics = vi.hoisted(() => vi.fn());
vi.mock("../lib/api", () => ({ getEvaluationMetrics }));

describe("EvaluationPage", () => {
  beforeEach(() => {
    getEvaluationMetrics.mockResolvedValue({
      benchmark_only: true, sample_count: 1, truncated: false, reference_char_count: 100, edit_distance: 8, cer: 0.08,
      substitution_count: 4, omission_count: 2, addition_count: 2, missed_line_count: 1, guessed_line_count: 0, reordered_line_count: 0,
      low_confidence_item_count: 5, low_confidence_hit_count: 4, low_confidence_hit_rate: 0.8,
      average_candidate_seconds: 12, average_review_seconds: 90, average_turnaround_seconds: 300,
      review_sample_count: 1, confirmed_last_hour: 1, pages_per_active_hour: 40,
      groups: [{ provider: "provider", model: "model", prompt_version: "prompt-v1", sample_count: 1, reference_char_count: 100, edit_distance: 8, cer: 0.08 }],
      trend: [],
      samples: [{ document_id: "doc", document_title: "基准手稿", page_id: "page", page_no: 1, provider: "provider", model: "model", prompt_version: "prompt-v1", cer: 0.08, edit_distance: 8, reference_char_count: 100, candidate_seconds: 12, review_seconds: 90, turnaround_seconds: 300, missed_lines: 1, guessed_lines: 0, reordered_lines: 0, low_confidence_item_count: 5, low_confidence_hit_count: 4, finalized_at: "2026-01-01T00:00:00Z" }],
    });
  });

  it("renders candidate quality and real review activity metrics", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<QueryClientProvider client={client}><MemoryRouter><EvaluationPage /></MemoryRouter></QueryClientProvider>);
    expect(await screen.findAllByText("8.0%")).not.toHaveLength(0);
    expect(screen.getByText("80.0%")).toBeInTheDocument();
    expect(screen.getByText("1.5 分")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "基准手稿 · 第 1 页" })).toHaveAttribute("href", "/review/doc/page");
  });
});
