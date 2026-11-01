"use client";
import { zodResolver } from "@hookform/resolvers/zod";
import { CheckCircle } from "@phosphor-icons/react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useEffect, useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { Button } from "@/components/ui/Button";
import { Input, Select, Textarea } from "@/components/ui/Input";
import { languages, uploadCategories } from "@/config/upload";
import { publishVideo, saveDraft, uploadAsset } from "@/services/upload";
import type { UploadMeta, UploadPhase } from "@/types";
import { CaptionsEditor, type CaptionDraft } from "./CaptionsEditor";
import { DropZone } from "./DropZone";
import { uploadSchema, type UploadValues } from "./schema";
import { TagInput } from "./TagInput";
import { ThumbnailPicker } from "./ThumbnailPicker";
import { UploadProgress } from "./UploadProgress";
import { useUpload } from "./useUpload";

const visibilityHelp = { private: "Only you can watch", unlisted: "Anyone with the link can watch", public: "Everyone can find and watch" } as const;

export function UploadView() {
  const qc = useQueryClient();
  const u = useUpload();
  const [thumbFile, setThumbFile] = useState<File | null>(null);
  const [thumb, setThumb] = useState<string | null>(null);
  const [captions, setCaptions] = useState<CaptionDraft[]>([]);
  const [queued, setQueued] = useState<UploadValues | null>(null);

  const { register, handleSubmit, control, getValues, setValue, formState: { errors } } = useForm<UploadValues>({
    resolver: zodResolver(uploadSchema),
    defaultValues: { title: "", description: "", category: "Entertainment", tags: [], visibility: "private", comments: "on", language: "en", ageGate: true },
  });

  const toMeta = (v: UploadValues, extra: Partial<UploadMeta> = {}): UploadMeta => ({
    title: v.title, description: v.description, category: v.category, tags: v.tags, visibility: v.visibility,
    comments: v.comments, language: v.language, ageGate: v.ageGate, captions: [], ...extra,
  });

  const publish = useMutation({
    mutationFn: async (v: UploadValues) => {
      const s = u.session!;
      const thumbnail = thumbFile ? await uploadAsset(s.uploadId, "thumbnail", thumbFile) : (thumb ?? undefined);
      const caps = await Promise.all(captions.map(async (c) => ({ lang: c.lang, assetId: await uploadAsset(s.uploadId, "caption", c.file, c.lang) })));
      // `consent` is the 18+/consent/rights declaration. The API requires it to be true and logs it with the publish.
      await publishVideo(s.videoId, toMeta(v, { thumbnail, captions: caps, consent: v.consent }));
      return s.videoId;
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["studio"] }),
  });
  const draft = useMutation({
    mutationFn: () => {
      const v = getValues();
      return saveDraft(u.session!.videoId, { title: v.title, description: v.description, category: v.category, tags: v.tags, visibility: v.visibility, comments: v.comments, language: v.language, ageGate: v.ageGate });
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["studio"] }),
  });

  // "Publish when ready": run as soon as encoding finishes.
  useEffect(() => {
    if (queued && u.phase === "ready" && !publish.isPending) {
      const v = queued;
      setQueued(null);
      publish.mutate(v);
    }
  }, [queued, u.phase, publish]);

  const phase: UploadPhase = publish.isSuccess ? "completed" : publish.isPending ? "publishing" : u.phase;

  function startFile(f: File) {
    if (!getValues("title")) setValue("title", f.name.replace(/\.[^.]+$/, "").slice(0, 100));
    publish.reset();
    u.start(f);
  }
  function reset() {
    setQueued(null);
    publish.reset();
    draft.reset();
    setThumb(null);
    setThumbFile(null);
    setCaptions([]);
    u.reset();
  }
  const onSubmit = (v: UploadValues) => (u.phase === "ready" ? publish.mutate(v) : setQueued(v));

  if (phase === "completed" && u.session) {
    return (
      <div className="mx-auto grid max-w-lg place-items-center gap-3 rounded-card border border-line bg-card px-6 py-16 text-center">
        <CheckCircle size={44} weight="fill" aria-hidden="true" />
        <h1 className="text-xl font-bold">Your video is published</h1>
        <p className="text-sm text-fg-2">It may take a moment to appear everywhere.</p>
        <div className="flex flex-wrap justify-center gap-2 pt-2">
          <Link href="/studio/videos" className="inline-flex h-11 items-center rounded-full bg-accent px-6 text-sm font-semibold hover:bg-accent-hover">Open Studio</Link>
          <Link href={`/watch/${u.session.videoId}`} className="inline-flex h-11 items-center rounded-full border border-line bg-surface px-6 text-sm font-semibold hover:bg-line">Watch</Link>
          <Button variant="ghost" onClick={reset}>Upload another</Button>
        </div>
      </div>
    );
  }

  if (phase === "idle" || phase === "cancelled") {
    return (
      <div className="mx-auto max-w-3xl">
        <h1 className="mb-5 text-2xl font-extrabold">Upload video</h1>
        <DropZone onFile={startFile} notice={phase === "cancelled" ? "Upload cancelled. You can start again." : null} />
      </div>
    );
  }

  const busy = publish.isPending;
  const waiting = Boolean(queued);
  return (
    <div className="mx-auto max-w-6xl">
      <h1 className="mb-5 text-2xl font-extrabold">Upload details</h1>
      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_340px]">
        <div className="order-2 lg:order-1">
          <form onSubmit={handleSubmit(onSubmit)} noValidate className="space-y-5">
            <fieldset disabled={busy} className="min-w-0 space-y-5">
              <legend className="sr-only">Video details</legend>
              <Input label="Title" maxLength={100} error={errors.title?.message} {...register("title")} />
              <Textarea label="Description" rows={5} error={errors.description?.message} {...register("description")} />
              <div className="grid gap-4 sm:grid-cols-2">
                <Select label="Category" error={errors.category?.message} {...register("category")}>
                  {uploadCategories.map((c) => <option key={c} value={c}>{c}</option>)}
                </Select>
                <Select label="Language" {...register("language")}>
                  {languages.map((l) => <option key={l.code} value={l.code}>{l.label}</option>)}
                </Select>
              </div>
              <Controller control={control} name="tags" render={({ field, fieldState }) => <TagInput value={field.value} onChange={field.onChange} error={fieldState.error?.message} />} />

              <fieldset className="space-y-2">
                <legend className="mb-1 text-sm font-medium">Visibility</legend>
                {(["private", "unlisted", "public"] as const).map((v) => (
                  <label key={v} className="flex cursor-pointer items-start gap-3 rounded-xl border border-line bg-card p-3 text-sm has-[:checked]:border-white">
                    <input type="radio" value={v} className="mt-0.5 size-5 accent-accent" {...register("visibility")} />
                    <span><span className="block font-semibold capitalize">{v}</span><span className="text-xs text-fg-2">{visibilityHelp[v]}</span></span>
                  </label>
                ))}
              </fieldset>

              <Select label="Comments" {...register("comments")}>
                <option value="on">Allow all comments</option>
                <option value="review">Hold comments for review</option>
                <option value="off">Turn off comments</option>
              </Select>
              <CaptionsEditor value={captions} onChange={setCaptions} />

              <div className="space-y-3 text-sm">
                <label className="flex cursor-pointer gap-3">
                  <input type="checkbox" className="mt-0.5 size-5 shrink-0 accent-accent" {...register("ageGate")} />
                  <span>Ask viewers to confirm they’re 18+ before this video plays.</span>
                </label>
                <div>
                  <label className="flex cursor-pointer gap-3">
                    <input type="checkbox" className="mt-0.5 size-5 shrink-0 accent-accent" {...register("consent")} />
                    <span>I confirm that everyone appearing in this video is 18 or older, that I have their consent, and that I have the right to publish it. See the <Link href="/community-guidelines" className="underline">Community Guidelines</Link>.</span>
                  </label>
                  {errors.consent && <p role="alert" className="mt-1 text-xs text-accent">{errors.consent.message}</p>}
                </div>
              </div>
            </fieldset>

            {(publish.isError || draft.isError) && <p role="alert" className="text-sm text-accent">{publish.isError ? "Couldn’t publish your video. Please try again." : "Couldn’t save your draft."}</p>}
            {draft.isSuccess && !draft.isPending && <p role="status" className="text-sm text-fg-2">Draft saved.</p>}
            {waiting && <p role="status" className="text-sm text-fg-2">Your video will be published automatically when processing finishes.</p>}
            <div className="flex flex-wrap gap-2">
              <Button type="submit" loading={busy} disabled={waiting || phase === "failed" || (!u.session)}>
                {phase === "ready" ? "Publish" : waiting ? "Waiting to publish…" : "Publish when ready"}
              </Button>
              <Button variant="secondary" loading={draft.isPending} disabled={!u.session || busy} onClick={() => draft.mutate()}>Save draft</Button>
            </div>
          </form>
        </div>

        <aside className="order-1 space-y-5 lg:order-2 lg:sticky lg:top-24 lg:self-start">
          <UploadProgress phase={phase} progress={u.progress} serverProgress={u.serverStatus?.progress ?? 0} fileName={u.fileInfo?.name ?? ""}
            error={u.error} failedAtProcessing={u.processingFailed} onCancel={() => { u.cancel(); setQueued(null); }} onRetry={u.retry} onReset={reset} />
          <ThumbnailPicker candidates={u.serverStatus?.thumbnails ?? []} selected={thumb} onSelect={setThumb} customFile={thumbFile} onCustomFile={setThumbFile} />
        </aside>
      </div>
    </div>
  );
}
