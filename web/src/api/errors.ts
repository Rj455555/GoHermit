export type ApiErrorCode =
  | 'invalid_path'
  | 'aborted'
  | 'network_error'
  | 'http_error'
  | 'invalid_content_type'
  | 'response_too_large'
  | 'invalid_json'
  | 'invalid_response'

export class ApiError extends Error {
  readonly code: ApiErrorCode
  readonly status: number | undefined

  constructor(code: ApiErrorCode, status?: number) {
    super(code)
    this.name = 'ApiError'
    this.code = code
    this.status = status
  }

  toJSON() {
    return { name: this.name, code: this.code, status: this.status }
  }
}

export class DecodeError extends ApiError {
  constructor() {
    super('invalid_response')
    this.name = 'DecodeError'
  }
}
