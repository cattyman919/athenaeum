"use client";

// Admin "Add Book" dialog (docs/11 §6, docs/07 screen 6a) hosting the shared AddBookFlow.
// Admin-only → English (docs/14 §1).

import * as React from "react";
import { BookPlus } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { AddBookFlow } from "./add-book-flow";

export function AddBookDialog() {
  const [open, setOpen] = React.useState(false);
  const close = React.useCallback(() => setOpen(false), []);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button data-testid="add-book-open">
          <BookPlus className="h-4 w-4" aria-hidden />
          Add book
        </Button>
      </DialogTrigger>
      <DialogContent
        className="top-[5%] max-h-[90vh] max-w-2xl overflow-y-auto p-6"
        data-testid="add-book-dialog"
      >
        <DialogTitle className="text-lg font-semibold">Add book</DialogTitle>
        <DialogDescription className="mb-4 text-sm text-on-surface-variant">
          Type or scan the ISBN to prefill from Open Library, then confirm the record and its
          copies.
        </DialogDescription>
        <AddBookFlow onClose={close} />
      </DialogContent>
    </Dialog>
  );
}
