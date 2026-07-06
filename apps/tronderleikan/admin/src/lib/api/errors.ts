// Klient-trygg: ingen server-only-import. Kastes av BFF-en (serverfunksjoner)
// og fanges i loadere/komponenter (som kjører både på server og klient).
export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}
