/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// typesafe.ts — TypeSafe System One HTTP client: build questions, POST to the
// evaluate endpoint, decode per-question answers (noul probability / choice).

export const ENDPOINT = "https://api.typesafe.ai/v1/systemone";
export const DEFAULT_MODEL = "jev-latest";

/** A single System One question (noul yes/no, or choice over a label set). */
export interface Question {
	type: "noul" | "choice";
	instructions: string;
	criteria: Record<string, string>;
}

/** Build a yes/no question; criteria maps "true"/"false" to their meanings. */
export function noul(instructions: string, criteria: Record<string, string>): Question {
	return { type: "noul", instructions, criteria };
}

/** Build a choice question; criteria maps option name -> description. */
export function choice(instructions: string, criteria: Record<string, string>): Question {
	return { type: "choice", instructions, criteria };
}

/** Raw decoded answer for one question id. */
export interface Answer {
	/** noul probability that the answer is "true". */
	noul: number;
	/** choice-selected label. */
	choice: string;
	/** per-option probabilities. */
	probabilities: Record<string, number>;
	/** model-reported confidence. */
	confidence: number;
}

/** Token usage for one request. */
export interface Usage {
	input_tokens: number;
	output_tokens: number;
}

/** Error raised when the upstream reports a context/token limit. */
export class TokenLimitError extends Error {}

/** Thrown for non-retryable upstream errors, carrying the upstream message. */
export class ApiError extends Error {
	constructor(
		message: string,
		readonly status: number,
	) {
		super(message);
	}
}

interface ApiResponse {
	answers?: Record<string, unknown>;
	usage?: Usage;
	error?: { message?: string };
}

function isTokenLimitMessage(msg: string): boolean {
	return /max_tokens|tokens_exceeded|context/i.test(msg);
}

function sleep(ms: number): Promise<void> {
	const { promise, resolve } = Promise.withResolvers<void>();
	setTimeout(resolve, ms);
	return promise;
}

/** Client for the TypeSafe evaluate endpoint, with retry + in-memory cache. */
export class Client {
	private readonly apiKey: string;
	private readonly model: string;
	private readonly retries = 3;
	/** Simple in-process response cache keyed by serialized request bytes. */
	private readonly cache = new Map<string, { answers: Record<string, Answer>; usage: Usage }>();
	/** When true, read-through cache is enabled. */
	cacheOn = true;
	/** When true, bypass reads (but still writes) — refresh semantics. */
	refresh = false;

	constructor(apiKey: string, model = DEFAULT_MODEL) {
		this.apiKey = apiKey;
		this.model = model || DEFAULT_MODEL;
	}

	/** Evaluate all questions against the same state in one request. */
	async evaluate(
		state: unknown,
		questions: Record<string, Question>,
	): Promise<{ answers: Record<string, Answer>; usage: Usage }> {
		const body = { state, model: this.model, questions };
		const key = JSON.stringify(body);
		if (this.cacheOn && !this.refresh) {
			const hit = this.cache.get(key);
			if (hit) return { answers: hit.answers, usage: hit.usage };
		}
		const payload = key;

		let lastErr: unknown;
		for (let attempt = 0; attempt <= this.retries; attempt++) {
			if (attempt > 0) {
				const is429 = lastErr instanceof ApiError && lastErr.status === 429;
				await sleep(is429 ? Math.min(2000 * attempt, 8000) : Math.min(500 * 2 ** attempt, 4000));
			}
			let resp: Response;
			try {
				resp = await fetch(ENDPOINT, {
					method: "POST",
					headers: {
						Authorization: `Bearer ${this.apiKey}`,
						"Content-Type": "application/json",
					},
					body: payload,
				});
			} catch (err) {
				lastErr = err;
				continue;
			}
			const raw = await resp.text();
			if (resp.status === 200) {
				let out: ApiResponse;
				try {
					out = JSON.parse(raw) as ApiResponse;
				} catch (err) {
					throw new ApiError(`failed to parse response: ${String(err)}`, resp.status);
				}
				const answers = decodeAnswers(out.answers ?? {});
				const usage = out.usage ?? { input_tokens: 0, output_tokens: 0 };
				if (this.cacheOn) this.cache.set(key, { answers, usage });
				return { answers, usage };
			}
			// Retryable: 429 + 5xx.
			if (resp.status === 429 || resp.status >= 500) {
				lastErr = new ApiError(`HTTP ${resp.status}: ${truncate(raw)}`, resp.status);
				continue;
			}
			// Fatal — surface upstream message; classify token-limit for callers.
			const msg = parseErrorMessage(raw) || `HTTP ${resp.status}`;
			if (isTokenLimitMessage(msg)) throw new TokenLimitError(msg);
			throw new ApiError(msg, resp.status);
		}
		throw new ApiError(`request failed after ${this.retries} retries: ${String(lastErr)}`, 0);
	}
}

/** Decode the answers map into typed Answer objects per question id. */
function decodeAnswers(raw: Record<string, unknown>): Record<string, Answer> {
	const out: Record<string, Answer> = {};
	for (const [id, v] of Object.entries(raw)) {
		const r = (v ?? {}) as Record<string, unknown>;
		out[id] = {
			noul: num(r.noul ?? r.probability ?? r.p),
			choice: str(r.choice ?? r.selected ?? r.answer),
			probabilities: (r.probabilities as Record<string, number>) ?? {},
			confidence: num(r.confidence),
		};
	}
	return out;
}

function num(v: unknown): number {
	return typeof v === "number" ? v : 0;
}
function str(v: unknown): string {
	return typeof v === "string" ? v : "";
}
function parseErrorMessage(raw: string): string {
	try {
		const o = JSON.parse(raw) as ApiResponse;
		return o.error?.message ?? "";
	} catch {
		return truncate(raw);
	}
}
function truncate(s: string): string {
	return s.length > 300 ? `${s.slice(0, 300)}…` : s;
}
