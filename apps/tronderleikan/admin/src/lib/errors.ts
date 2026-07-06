// Trekker ut en lesbar feilmelding fra en ukjent kastet verdi (BFF-feil bevarer
// tjenestens {"error": "..."} via ApiError.message).
export function errorMessage(err: unknown): string {
  if (err instanceof Error && err.message) return err.message
  return 'Noe gikk galt. Prøv igjen.'
}
