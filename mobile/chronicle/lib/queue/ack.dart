/// The only way a capture may become [QueueStatus.acknowledged].
///
/// A memo the server never received must never be marked sent, and the
/// converse matters just as much: a memo the server DID receive that this
/// client cannot prove it received must not be marked sent either. Both are
/// answered by requiring proof, not a status code, before anything is
/// written to `upload.json`.
library;

import 'package:chronicle_api/api.dart';

import '../capture/capture_record.dart';

/// Proof that the server holds exactly the bytes this capture declared.
///
/// The only way to get one is [verifyAck], and the only thing the queue
/// engine does with one is fold it into a [QueueRecord] on the ack path --
/// there is no other route to [QueueStatus.acknowledged] anywhere in this
/// package.
class Ack {
  const Ack._(this.memoId, this.byteSize, this.contentHash);

  final String memoId;
  final int byteSize;
  final String contentHash;
}

/// Verifies a server response against what THIS capture actually holds.
///
/// Checking against [record] rather than trusting the response's own
/// `status` field is what makes this a verification rather than a courier:
/// `complete` on its own is not enough, because it says nothing about
/// WHICH memo completed. Every one of these is "not an ack, keep trying" --
/// the default for anything this function does not recognise:
///
/// * `status != complete` (a plain `incomplete` is a resume, handled
///   elsewhere -- never an ack);
/// * `complete` with no `memo` attached;
/// * a `memo` whose `content_hash` or `byte_size` does not match [record]'s.
///
/// This is the only place in the queue that check is made, and it is made
/// on every response that claims to be complete.
Ack? verifyAck(UploadState state, CaptureRecord record) {
  if (state.status != UploadStateStatusEnum.complete) return null;
  final memo = state.memo;
  if (memo == null || memo.id.isEmpty) return null;
  final expectedHash = record.contentHash;
  final expectedSize = record.byteSize;
  if (expectedHash == null || expectedSize == null) return null;
  if (memo.contentHash != expectedHash) return null;
  if (memo.byteSize != expectedSize) return null;
  return Ack._(memo.id, memo.byteSize, memo.contentHash);
}
