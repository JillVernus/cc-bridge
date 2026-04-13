export const appendUniqueHeaderRule = (rules: string[], candidate: string): string[] => {
  const normalizedCandidate = candidate.trim()
  if (normalizedCandidate === '') {
    return rules
  }

  if (rules.some(rule => rule.trim().toLowerCase() === normalizedCandidate.toLowerCase())) {
    return rules
  }

  return [...rules, normalizedCandidate]
}
