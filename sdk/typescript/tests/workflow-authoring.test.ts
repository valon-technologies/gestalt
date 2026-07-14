import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import {
  buildWorkflowFromLoweringCase,
  canonicalWorkflowDefinitionSpec,
  defineWorkflow,
  event,
  resolveWorkflowDefinitionSpec,
  schedule,
} from "../src/workflow-authoring.ts";

const fixtureDir = join(
  dirname(fileURLToPath(import.meta.url)),
  "../../fixtures/workflow-authoring",
);
const loweringContract = JSON.parse(
  readFileSync(join(fixtureDir, "lowering-contract.json"), "utf8"),
) as {
  cases: Array<{
    name: string;
    init: { id: string; runAs: string; paused?: boolean };
    activations?: Array<Record<string, unknown>>;
    steps?: Array<Record<string, unknown>>;
    expectedSpec: Record<string, unknown>;
  }>;
};

test("defineWorkflow requires runAs", () => {
  expect(() => defineWorkflow({ id: "demo", runAs: "" })).toThrow(
    "defineWorkflow requires runAs",
  );
});

test("typed workflow builder matches extract row example", () => {
  const spec = defineWorkflow({
    id: "extractRow",
    runAs: "service_account:deal-hub-extraction",
  })
    .on(
      event("deal_hub.analyses.extract.requested", (activationEvent) => ({
        analysisId: activationEvent.data.analysisId,
      })),
    )
    .step("extract", {
      app: {
        name: "dealHub",
        operation: "analyses.extractRowWorkflow",
        input: (scope) => ({
          analysisId: scope.input.analysisId,
        }),
      },
    })
    .toSpec();

  expect(canonicalWorkflowDefinitionSpec(spec)).toEqual(
    loweringContract.cases.find((item) => item.name === "extract_row")?.expectedSpec,
  );
});

for (const caseData of loweringContract.cases) {
  test(`golden fixture: ${caseData.name}`, () => {
    const spec = buildWorkflowFromLoweringCase(caseData).toSpec();
    expect(canonicalWorkflowDefinitionSpec(spec)).toEqual(caseData.expectedSpec);
  });
}

test("resolveWorkflowDefinitionSpec accepts builders and raw specs", () => {
  const builder = defineWorkflow({
    id: "extractRow",
    runAs: "service_account:deal-hub-extraction",
  }).on(
    schedule("0 2 * * *", () => ({
      reason: "nightly",
    })),
  );

  const fromBuilder = resolveWorkflowDefinitionSpec(builder);
  const fromSpec = resolveWorkflowDefinitionSpec(fromBuilder);
  expect(fromBuilder.activations?.[0]?.schedule?.cron).toBe("0 2 * * *");
  expect(fromSpec.id).toBe("extractRow");
});
