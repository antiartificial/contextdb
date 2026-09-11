export function normalizeBeliefAudit(audit) {
  const epistemics = audit?.epistemics
  if (!epistemics) return audit
  epistemics.source ||= {}
  epistemics.source.labels ||= []
  epistemics.confidence_timeline ||= []
  epistemics.source_trust_timeline ||= []
  epistemics.contradiction_paths ||= []
  epistemics.graph_context ||= []
  return audit
}
