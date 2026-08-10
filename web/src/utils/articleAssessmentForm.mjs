/**
 * Reports whether a human assessor has entered anything that could be saved.
 * A completely blank form may be left through the "previous article" action
 * without turning the optional article into a required label.
 */
export function hasArticleAssessmentInput(form) {
  const scores = form?.scores || {}
  return Boolean(
    form?.unjudgeable
    || form?.extractionBad
    || String(form?.reason || '').trim()
    || form?.genre
    || [scores.quality, scores.depth, scores.evergreen].some(value => value !== null && value !== undefined && value !== ''),
  )
}
