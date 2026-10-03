"use client";

import { QRCodeSVG } from "qrcode.react";
import { useEffect, type MouseEvent } from "react";
import { Button } from "@/components/ui";
import type { Ticket } from "@/lib/api";
import { formatDateTime } from "@/lib/format";

const STATUS_LABEL = { VALID: "Valid", USED: "Used", VOID: "Void" } as const;

export function TicketQrModal({ ticket, onClose }: { ticket: Ticket; onClose: () => void }) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.body.style.overflow = "hidden";
    window.addEventListener("keydown", onKey);
    return () => {
      document.body.style.overflow = "";
      window.removeEventListener("keydown", onKey);
    };
  }, [onClose]);

  function backdropClick(e: MouseEvent<HTMLDivElement>) {
    if (e.target === e.currentTarget) onClose();
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/70 p-4 backdrop-blur-sm"
      role="dialog"
      aria-modal="true"
      aria-labelledby="ticket-qr-title"
      onClick={backdropClick}
    >
      <div className="relative w-full max-w-md rounded-2xl border border-slate-200 bg-white p-6 shadow-xl">
        <button
          type="button"
          className="absolute right-3 top-3 rounded-lg p-2 text-slate-500 hover:bg-slate-100 hover:text-slate-900"
          aria-label="Close"
          onClick={onClose}
        >
          <span className="text-xl leading-none">×</span>
        </button>
        <div className="space-y-5 text-center">
          <div>
            <h2 id="ticket-qr-title" className="text-lg font-semibold text-slate-900">
              {ticket.event_title}
            </h2>
            <p className="mt-1 text-sm text-slate-500">
              {ticket.venue} · {formatDateTime(ticket.starts_at)}
            </p>
          </div>
          <div className="mx-auto inline-block rounded-xl border-4 border-slate-900 bg-white p-4">
            <QRCodeSVG value={ticket.code} size={280} level="M" bgColor="#ffffff" fgColor="#0f172a" />
          </div>
          <div className="space-y-1 text-sm text-slate-700">
            <p>
              Seat <span className="font-semibold">{ticket.seat_label}</span> · {ticket.zone}
            </p>
            <p>Status: {STATUS_LABEL[ticket.status]}</p>
            <p className="break-all font-mono text-xs text-slate-500">{ticket.code}</p>
          </div>
          <Button variant="secondary" className="w-full" onClick={onClose}>
            Close
          </Button>
        </div>
      </div>
    </div>
  );
}
