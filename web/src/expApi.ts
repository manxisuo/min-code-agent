import type { ExperimentAggregate, ExperimentDetail } from "./expTypes";
import { fetchJSON } from "./api";

export function apiExperiments() {
  return fetchJSON<{ experiments: ExperimentAggregate[]; root?: string }>(
    "/api/experiments",
  );
}

export function apiExperiment(name: string) {
  return fetchJSON<ExperimentDetail>(
    "/api/experiments/" + encodeURIComponent(name),
  );
}
