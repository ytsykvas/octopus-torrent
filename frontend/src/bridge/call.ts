// Calls a generated binding and turns every outcome into a Result: the Go answer is untrusted until
// decoded, and a rejection carries the AppError Go formatted into the Error message.
import { decodeDto, fromRejection, type Result } from './contract/errors'
import type { Decoder } from './contract/decode'

export async function callGo<T>(invoke: () => Promise<unknown>, decoder: Decoder<T>): Promise<Result<T>> {
  let answer: unknown
  try {
    answer = await invoke()
  } catch (reason) {
    return { ok: false, error: fromRejection(reason) }
  }
  return decodeDto(decoder, answer)
}
