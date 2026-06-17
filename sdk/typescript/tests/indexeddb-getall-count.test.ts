import type { GetAllOptions, ObjectStore } from "../src/providers/indexeddb.ts";

type ObjectStoreGetAllOptions = Parameters<ObjectStore["getAll"]>[1];
type CountOptionAssignable = { count: number } extends GetAllOptions ? true : false;
type WrapperOptionsMatchProto = { count?: number } extends ObjectStoreGetAllOptions ? true : false;

const _assertCountOnGetAllOptions: CountOptionAssignable = true;
const _assertWrapperOptions: WrapperOptionsMatchProto = true;

void _assertCountOnGetAllOptions;
void _assertWrapperOptions;
