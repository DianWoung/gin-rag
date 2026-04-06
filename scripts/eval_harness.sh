#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

MYSQL_DSN="${MYSQL_DSN:-}"
HARNESS_SAMPLE_FILE="${HARNESS_SAMPLE_FILE:-eval/harness/sample_ids.txt}"
HARNESS_SAMPLE_IDS="${HARNESS_SAMPLE_IDS:-}"
HARNESS_BASELINE_FILE="${HARNESS_BASELINE_FILE:-eval/harness/baseline.json}"
HARNESS_REPORT_DIR="${HARNESS_REPORT_DIR:-eval/reports}"
HARNESS_INIT_BASELINE="${HARNESS_INIT_BASELINE:-0}"
HARNESS_REQUIRE_MANUAL="${HARNESS_REQUIRE_MANUAL:-1}"
HARNESS_MAX_UNLABELED_RATIO="${HARNESS_MAX_UNLABELED_RATIO:-0.20}"
HARNESS_GROUNDED_MAX_DROP="${HARNESS_GROUNDED_MAX_DROP:-0.03}"
HARNESS_RETRIEVAL_MAX_DROP="${HARNESS_RETRIEVAL_MAX_DROP:-0.03}"
HARNESS_TABLE_CELL_MAX_DROP="${HARNESS_TABLE_CELL_MAX_DROP:-0.05}"

if [[ -z "$MYSQL_DSN" ]]; then
  echo "MYSQL_DSN is required" >&2
  exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required" >&2
  exit 1
fi

log() {
  printf '[harness] %s\n' "$*"
}

usage() {
  cat <<'EOF'
Usage:
  MYSQL_DSN='...' bash ./scripts/eval_harness.sh

Optional environment variables:
  HARNESS_SAMPLE_FILE            default: eval/harness/sample_ids.txt
  HARNESS_SAMPLE_IDS             comma-separated sample ids (overrides sample file)
  HARNESS_BASELINE_FILE          default: eval/harness/baseline.json
  HARNESS_REPORT_DIR             default: eval/reports
  HARNESS_INIT_BASELINE          1 to write/update baseline with current run
  HARNESS_REQUIRE_MANUAL         default: 1 (fail when manual aggregate is missing)
  HARNESS_MAX_UNLABELED_RATIO    default: 0.20
  HARNESS_GROUNDED_MAX_DROP      default: 0.03
  HARNESS_RETRIEVAL_MAX_DROP     default: 0.03
  HARNESS_TABLE_CELL_MAX_DROP    default: 0.05
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

sample_ids=()
if [[ -n "$HARNESS_SAMPLE_IDS" ]]; then
  IFS=',' read -r -a raw_ids <<<"$HARNESS_SAMPLE_IDS"
  for id in "${raw_ids[@]}"; do
    id="$(echo "$id" | xargs)"
    if [[ -n "$id" ]]; then
      sample_ids+=("$id")
    fi
  done
else
  if [[ ! -f "$HARNESS_SAMPLE_FILE" ]]; then
    echo "sample file not found: $HARNESS_SAMPLE_FILE" >&2
    exit 1
  fi
  while IFS= read -r line; do
    line="$(echo "$line" | xargs)"
    if [[ -z "$line" || "$line" == \#* ]]; then
      continue
    fi
    sample_ids+=("$line")
  done <"$HARNESS_SAMPLE_FILE"
fi

if [[ "${#sample_ids[@]}" -eq 0 ]]; then
  echo "no sample ids provided" >&2
  exit 1
fi

mkdir -p "$HARNESS_REPORT_DIR" "$(dirname "$HARNESS_BASELINE_FILE")"

log "running compare-samples for ${#sample_ids[@]} samples"
compare_output="$(
  MYSQL_DSN="$MYSQL_DSN" \
  go run ./cmd/evalctl compare-samples "${sample_ids[@]}"
)"

current_metrics="$(
  jq -c '
    {
      sample_count: (.sample_ids | length),
      unlabeled_ratio: (
        if (.by_sample | length) == 0 then 1
        else (([.by_sample[] | select((.manual_count // 0) == 0)] | length) / (.by_sample | length))
        end
      ),
      annotation_count: (.manual_aggregate.annotation_count // 0),
      manual_grounded_answer: (.manual_aggregate.metric_scores.grounded_answer // null),
      manual_retrieval_relevance: (.manual_aggregate.metric_scores.retrieval_relevance // null),
      auto_captured_grounded_answer: (
        [.aggregate[] | select(.target=="captured" and .metric=="grounded_answer") | .average_score] | first // null
      ),
      auto_captured_retrieval_precision_at_k: (
        [.aggregate[] | select(.target=="captured" and .metric=="retrieval_precision_at_k") | .average_score] | first // null
      ),
      auto_captured_table_cell_accuracy: (
        [.aggregate[] | select(.target=="captured" and .metric=="table_cell_accuracy") | .average_score] | first // null
      )
    }
  ' <<<"$compare_output"
)"

if [[ "$HARNESS_INIT_BASELINE" == "1" ]]; then
  jq -n \
    --arg generated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --arg sample_file "$HARNESS_SAMPLE_FILE" \
    --argjson metrics "$current_metrics" \
    '{
      generated_at: $generated_at,
      sample_file: $sample_file,
      metrics: $metrics
    }' >"$HARNESS_BASELINE_FILE"
  log "baseline initialized at $HARNESS_BASELINE_FILE"
fi

if [[ ! -f "$HARNESS_BASELINE_FILE" ]]; then
  echo "baseline file missing: $HARNESS_BASELINE_FILE (set HARNESS_INIT_BASELINE=1 to initialize)" >&2
  exit 1
fi

baseline_metrics="$(jq -c '.metrics' "$HARNESS_BASELINE_FILE")"

report_file="$HARNESS_REPORT_DIR/harness_$(date +%Y%m%d_%H%M%S).json"

validation_report="$(
  jq -n \
    --arg generated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --arg baseline_file "$HARNESS_BASELINE_FILE" \
    --argjson baseline "$baseline_metrics" \
    --argjson current "$current_metrics" \
    --argjson max_unlabeled "$HARNESS_MAX_UNLABELED_RATIO" \
    --argjson max_grounded_drop "$HARNESS_GROUNDED_MAX_DROP" \
    --argjson max_retrieval_drop "$HARNESS_RETRIEVAL_MAX_DROP" \
    --argjson max_table_cell_drop "$HARNESS_TABLE_CELL_MAX_DROP" \
    --arg require_manual "$HARNESS_REQUIRE_MANUAL" \
    '
      def drop($base; $cur):
        if ($base == null or $cur == null) then null else ($base - $cur) end;
      def failed($name; $ok; $detail):
        {name:$name, ok:$ok, detail:$detail};

      ($current.unlabeled_ratio <= $max_unlabeled) as $ok_unlabeled
      | (($require_manual == "0") or ($current.annotation_count > 0 and $current.manual_grounded_answer != null and $current.manual_retrieval_relevance != null)) as $ok_manual_present
      | (drop($baseline.manual_grounded_answer; $current.manual_grounded_answer)) as $drop_manual_grounded
      | (drop($baseline.manual_retrieval_relevance; $current.manual_retrieval_relevance)) as $drop_manual_retrieval
      | (drop($baseline.auto_captured_table_cell_accuracy; $current.auto_captured_table_cell_accuracy)) as $drop_table_cell
      | (if $drop_manual_grounded == null then true else ($drop_manual_grounded <= $max_grounded_drop) end) as $ok_manual_grounded_drop
      | (if $drop_manual_retrieval == null then true else ($drop_manual_retrieval <= $max_retrieval_drop) end) as $ok_manual_retrieval_drop
      | (if $drop_table_cell == null then true else ($drop_table_cell <= $max_table_cell_drop) end) as $ok_table_cell_drop
      | (
          $baseline.auto_captured_grounded_answer != null
          and $baseline.manual_grounded_answer != null
          and $current.auto_captured_grounded_answer != null
          and $current.manual_grounded_answer != null
          and ($current.auto_captured_grounded_answer > $baseline.auto_captured_grounded_answer)
          and ($current.manual_grounded_answer < $baseline.manual_grounded_answer)
        ) as $direction_conflict_grounded
      | (
          $baseline.auto_captured_retrieval_precision_at_k != null
          and $baseline.manual_retrieval_relevance != null
          and $current.auto_captured_retrieval_precision_at_k != null
          and $current.manual_retrieval_relevance != null
          and ($current.auto_captured_retrieval_precision_at_k > $baseline.auto_captured_retrieval_precision_at_k)
          and ($current.manual_retrieval_relevance < $baseline.manual_retrieval_relevance)
        ) as $direction_conflict_retrieval
      | {
          generated_at: $generated_at,
          baseline_file: $baseline_file,
          baseline: $baseline,
          current: $current,
          checks: [
            failed("manual_present"; $ok_manual_present; ("require_manual=" + $require_manual + ", annotation_count=" + (($current.annotation_count // 0)|tostring))),
            failed("unlabeled_ratio"; $ok_unlabeled; ("ratio=" + (($current.unlabeled_ratio // 0)|tostring) + ", max=" + ($max_unlabeled|tostring))),
            failed("manual_grounded_drop"; $ok_manual_grounded_drop; ("drop=" + (($drop_manual_grounded // 0)|tostring) + ", max=" + ($max_grounded_drop|tostring))),
            failed("manual_retrieval_drop"; $ok_manual_retrieval_drop; ("drop=" + (($drop_manual_retrieval // 0)|tostring) + ", max=" + ($max_retrieval_drop|tostring))),
            failed("table_cell_accuracy_drop"; $ok_table_cell_drop; ("drop=" + (($drop_table_cell // 0)|tostring) + ", max=" + ($max_table_cell_drop|tostring))),
            failed("direction_conflict_grounded"; ($direction_conflict_grounded | not); ("conflict=" + ($direction_conflict_grounded|tostring))),
            failed("direction_conflict_retrieval"; ($direction_conflict_retrieval | not); ("conflict=" + ($direction_conflict_retrieval|tostring)))
          ]
        }
        | .overall_pass = ([.checks[] | .ok] | all)
    '
)"

echo "$validation_report" >"$report_file"
log "report written: $report_file"

if [[ "$(jq -r '.overall_pass' <<<"$validation_report")" != "true" ]]; then
  echo "$validation_report" | jq '.'
  echo "harness validation failed" >&2
  exit 1
fi

echo "$validation_report" | jq '.'
log "harness validation passed"
