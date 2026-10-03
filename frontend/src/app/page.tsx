"use client";

import { useState } from "react";
import { getHealth, type Health } from "@/lib/api";

type State =
  | { kind: "idle" }
  | { kind: "loading" }
  | { kind: "ok"; data: Health }
  | { kind: "error"; message: string };

const Dot = ({ ok }: { ok: boolean }) => (
  <span className={`inline-block h-3 w-3 rounded-full ${ok ? "bg-green-500" : "bg-red-500"}`} />
);

export default function Home() {
  const [state, setState] = useState<State>({ kind: "idle" });

  const load = async () => {
    setState({ kind: "loading" });
    try {
      const data = await getHealth();
      setState({ kind: "ok", data });
    } catch (e) {
      setState({ kind: "error", message: e instanceof Error ? e.message : "Unknown error" });
    }
  };

  return (
    <main className="mx-auto max-w-xl p-8 space-y-6">
      <h1 className="text-2xl font-bold">Ticket Booking — System Status</h1>

      {state.kind === "idle" && (
        <div className="space-y-3">
          <p className="text-gray-600">กดปุ่มด้านล่างเพื่อตรวจสอบสถานะการเชื่อมต่อระบบ</p>
          <button onClick={load} className="rounded bg-black px-4 py-2 text-sm text-white font-medium hover:bg-gray-800">
            ตรวจสอบสถานะระบบ
          </button>
        </div>
      )}

      {state.kind === "loading" && <p className="text-gray-500">กำลังตรวจสอบสถานะระบบ…</p>}

      {state.kind === "error" && (
        <div className="rounded border border-red-300 bg-red-50 p-4 space-y-2">
          <p className="font-medium text-red-700">ไม่สามารถติดต่อ backend ได้</p>
          <p className="text-sm text-red-600">{state.message}</p>
          <button onClick={load} className="rounded bg-red-600 px-3 py-1 text-sm text-white">
            ลองใหม่
          </button>
        </div>
      )}

      {state.kind === "ok" && (
        <div className="rounded border p-4 space-y-3">
          <Row label="Backend"  ok={true} value={state.data.status} />
          <Row label="Database" ok={state.data.db === "ok"} value={state.data.db} />
          <Row label="Redis"    ok={state.data.redis === "ok"} value={state.data.redis} />
          <button onClick={load} className="rounded border px-3 py-1 text-sm">รีเฟรช</button>
        </div>
      )}
    </main>
  );
}

function Row({ label, ok, value }: { label: string; ok: boolean; value: string }) {
  return (
    <div className="flex items-center justify-between">
      <span className="font-medium">{label}</span>
      <span className="flex items-center gap-2 text-sm">
        <Dot ok={ok} /> {value}
      </span>
    </div>
  );
}