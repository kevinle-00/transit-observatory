export type HttpResponseInit = ResponseInit

export class HttpResponse extends Response {
  constructor(body: unknown, init?: HttpResponseInit) {
    super(JSON.stringify(body), init)
  }
}
