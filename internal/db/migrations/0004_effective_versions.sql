DROP VIEW page_details;

-- A page's effective text is the version the review UI opens and downstream
-- search/export operations must use. final and manual are the same editing
-- tier: a manual draft saved after a final reopens the page for review.
CREATE VIEW effective_text_versions AS
SELECT
  v.id,
  v.document_id,
  v.page_id,
  v.kind,
  v.base_version_id,
  v.source_result_id,
  v.text,
  v.status,
  v.created_by,
  v.created_at
FROM text_versions v
WHERE v.page_id IS NOT NULL
  AND v.id = (
    SELECT v2.id
    FROM text_versions v2
    WHERE v2.page_id = v.page_id
    ORDER BY
      CASE v2.kind
        WHEN 'final' THEN 0
        WHEN 'manual' THEN 0
        WHEN 'candidate' THEN 2
        WHEN 'raw_selected' THEN 3
        ELSE 4
      END,
      julianday(v2.created_at) DESC,
      v2.created_at DESC,
      v2.rowid DESC
    LIMIT 1
  );

CREATE VIEW page_details AS
SELECT
  p.id            AS page_id,
  p.document_id,
  p.page_no,
  p.status        AS page_status,
  p.width,
  p.height,
  p.image_asset_id,
  p.thumb_asset_id,
  (SELECT COUNT(*)          FROM recognition_results r WHERE r.page_id = p.id) AS recognition_count,
  (SELECT MAX(r.confidence) FROM recognition_results r WHERE r.page_id = p.id) AS best_confidence,
  (SELECT run.provider FROM recognition_results r
     JOIN recognition_runs run ON run.id = r.run_id
     WHERE r.page_id = p.id ORDER BY r.created_at DESC LIMIT 1) AS last_provider,
  (SELECT run.model FROM recognition_results r
     JOIN recognition_runs run ON run.id = r.run_id
     WHERE r.page_id = p.id ORDER BY r.created_at DESC LIMIT 1) AS last_model,
  (SELECT MAX(r.created_at) FROM recognition_results r WHERE r.page_id = p.id) AS last_recognized_at,
  CASE WHEN ev.kind = 'candidate' THEN 1 ELSE 0 END AS has_candidate,
  CASE WHEN ev.kind = 'manual' THEN 1 ELSE 0 END AS has_manual,
  CASE WHEN ev.kind = 'final' THEN 1 ELSE 0 END AS has_final,
  p.updated_at
FROM pages p
LEFT JOIN effective_text_versions ev ON ev.page_id = p.id;

-- Existing databases may have indexed an older final even after a newer
-- manual draft was saved. Rebuild the FTS table from the effective versions.
DELETE FROM text_search;

INSERT INTO text_search(document_id, page_id, text_version_id, title, body)
SELECT ev.document_id, ev.page_id, ev.id, d.title, ev.text
FROM effective_text_versions ev
JOIN documents d ON d.id = ev.document_id
WHERE ev.kind IN ('candidate', 'manual', 'final');

-- Legacy rows may still say finalized even when a later manual draft reopened
-- one of their pages. Bring persisted document status in line with the new
-- effective-version rule during the same upgrade.
UPDATE documents
SET status = CASE
  WHEN page_count > 0
   AND (SELECT COUNT(*) FROM effective_text_versions ev
        WHERE ev.document_id = documents.id AND ev.kind = 'final') = page_count
    THEN 'finalized'
  WHEN EXISTS(SELECT 1 FROM effective_text_versions ev
              WHERE ev.document_id = documents.id AND ev.kind IN ('manual', 'final'))
    THEN 'reviewing'
  WHEN EXISTS(SELECT 1 FROM effective_text_versions ev
              WHERE ev.document_id = documents.id AND ev.kind IN ('candidate', 'raw_selected'))
    OR EXISTS(SELECT 1 FROM recognition_results r
              JOIN pages p ON p.id = r.page_id
              WHERE p.document_id = documents.id)
    THEN 'review_pending'
  ELSE 'ready'
END,
updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE status NOT IN ('importing', 'failed');
