import type { AcquireLockResult, IndexedDB } from "../src/index.ts";

type AcquireLockFn = IndexedDB["acquireLock"];
type ReleaseLockFn = IndexedDB["releaseLock"];

type AcquireLockParamsMatch =
  Parameters<AcquireLockFn> extends [string, string, number] ? true : false;
type AcquireLockReturnMatch =
  ReturnType<AcquireLockFn> extends Promise<AcquireLockResult> ? true : false;
type ReleaseLockParamsMatch =
  Parameters<ReleaseLockFn> extends [string, string] ? true : false;
type FencingTokenIsBigint =
  AcquireLockResult["fencingToken"] extends bigint ? true : false;

const _assertAcquireParams: AcquireLockParamsMatch = true;
const _assertAcquireReturn: AcquireLockReturnMatch = true;
const _assertReleaseParams: ReleaseLockParamsMatch = true;
const _assertFencingToken: FencingTokenIsBigint = true;

void _assertAcquireParams;
void _assertAcquireReturn;
void _assertReleaseParams;
void _assertFencingToken;
