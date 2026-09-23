/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// host.d.ts — minimal local typings for the Pi/OMP host API surface this
// extension uses. The real module is injected by the host at runtime
// (@earendil-works/pi-coding-agent), so this declaration only needs to cover
// what index.ts touches — it is NOT a full copy of the host API. `typebox` /
// `@sinclair/typebox` resolve to the real installed package (and to OMP's
// legacy shim in the host), so no stub is needed for it.

declare module "@earendil-works/pi-coding-agent" {
	import type { Static, TSchema } from "@sinclair/typebox";

	/** A single text content block in a message/tool result. */
	export interface TextContent {
		type: "text";
		text: string;
	}
	export interface ImageContent {
		type: "image";
		data?: string;
		mimeType?: string;
	}
	export type Content = TextContent | ImageContent;

	/** Result a tool returns to the model. */
	export interface AgentToolResult<TDetails = unknown> {
		content: Content[];
		details?: TDetails;
		isError?: boolean;
	}

	/** Streaming partial-result callback for tools. */
	export type AgentToolUpdateCallback<TDetails = unknown> = (partial: AgentToolResult<TDetails>) => void;

	/** UI context (subset used here). */
	export interface ExtensionUIContext {
		notify(message: string, level?: "info" | "warning" | "error"): void;
		select(message: string, options: string[]): Promise<string | undefined>;
		confirm(message: string): Promise<boolean>;
		/** Raw terminal input listener; returns an unsubscribe function. TUI only. */
		onTerminalInput(handler: (data: string) => { consume?: boolean; data?: string } | undefined | void): () => void;
		/** Set a widget above/below the editor (string[] or undefined to clear that key). */
		setWidget(
			key: string,
			content: string[] | undefined,
			options?: { placement?: "aboveEditor" | "belowEditor" },
		): void;
		/** Current editor text ("" when empty). */
		getEditorText(): string;
	}

	/** Context passed to event handlers / tool execute. */
	export interface ExtensionContext {
		ui: ExtensionUIContext;
		cwd: string;
		hasUI: boolean;
		mode: "tui" | "rpc" | "json" | "print";
		isIdle(): boolean;
		abort(): void;
		/** OMP-only: session-owned timer, auto-cleared on session_shutdown. */
		setTimeout?(cb: (...a: unknown[]) => void, ms?: number, ...a: unknown[]): unknown;
		setInterval?(cb: (...a: unknown[]) => void, ms?: number, ...a: unknown[]): unknown;
	}

	/** before_agent_start event. systemPrompt is string on Pi, string[] on OMP. */
	export interface BeforeAgentStartEvent {
		type: "before_agent_start";
		prompt: string;
		images?: ImageContent[];
		systemPrompt?: string | string[];
	}
	export interface BeforeAgentStartEventResult {
		message?: { customType: string; content: string | Content[]; display?: boolean; details?: unknown };
		systemPrompt?: string | string[];
	}

	/** tool_result event (can rewrite content). */
	export interface ToolResultEvent {
		type: "tool_result";
		toolName: string;
		toolCallId: string;
		input?: Record<string, unknown>;
		content?: Content[];
		isError?: boolean;
	}
	export interface ToolResultEventResult {
		content?: Content[];
		details?: unknown;
		isError?: boolean;
	}

	/** A registerable tool. Params typed via the TypeBox schema's inferred output. */
	export interface ToolDefinition<TParams extends TSchema = TSchema, TDetails = unknown> {
		name: string;
		label: string;
		description: string;
		parameters: TParams;
		/** "essential" = top-level tool; "discoverable" (extension default) = xd://-mounted / tool-search only. */
		loadMode?: "essential" | "discoverable";
		/** Registered but not auto-included in the initial active set. */
		defaultInactive?: boolean;
		execute(
			toolCallId: string,
			params: Static<TParams>,
			signal: AbortSignal | undefined,
			onUpdate: AgentToolUpdateCallback<TDetails> | undefined,
			ctx: ExtensionContext,
		): Promise<AgentToolResult<TDetails>>;
	}

	export type ExtensionHandler<E, R = undefined> = (event: E, ctx: ExtensionContext) => Promise<R | void> | R | void;

	/** The API object passed to the extension factory. */
	export interface ExtensionAPI {
		on(event: "before_agent_start", h: ExtensionHandler<BeforeAgentStartEvent, BeforeAgentStartEventResult>): void;
		on(event: "tool_result", h: ExtensionHandler<ToolResultEvent, ToolResultEventResult>): void;
		on(event: "session_start", h: ExtensionHandler<{ type: "session_start" }>): void;
		on(event: "session_shutdown", h: ExtensionHandler<{ type: "session_shutdown" }>): void;
		on(event: string, h: ExtensionHandler<unknown, unknown>): void;
		registerTool<TParams extends TSchema, TDetails = unknown>(tool: ToolDefinition<TParams, TDetails>): void;
		registerCommand(
			name: string,
			options: { description?: string; handler: (args: string, ctx: ExtensionContext) => void | Promise<void> },
		): void;
		sendMessage(
			message: { customType: string; content: string | Content[]; display?: boolean },
			options?: { triggerTurn?: boolean },
		): void;
		exec(command: string, args: string[], options?: { cwd?: string }): Promise<{ stdout: string; stderr: string; code: number }>;
		getActiveTools(): string[];
		setActiveTools(toolNames: string[]): void | Promise<void>;
	}
}
