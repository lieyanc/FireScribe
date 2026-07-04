import { useState, type KeyboardEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Plus, Tag as TagIcon, X } from "lucide-react";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "../ui/popover";
import { Separator } from "../ui/separator";
import { listTags, setDocumentTags, type Document, type Tag } from "../../lib/api";
import { cn } from "../../lib/utils";

export function TagChips({ tags, className }: { tags?: Tag[]; className?: string }) {
  if (!tags?.length) return null;
  return (
    <div className={cn("flex flex-wrap items-center gap-1", className)}>
      {tags.map((tag) => (
        <TagChip key={tag.id} name={tag.name} />
      ))}
    </div>
  );
}

export function TagChip({ name, onRemove }: { name: string; onRemove?: () => void }) {
  return (
    <span className="inline-flex h-6 max-w-full items-center gap-1 rounded-md border bg-secondary px-2 text-xs text-secondary-foreground">
      <TagIcon className="size-3 shrink-0 text-muted-foreground" />
      <span className="truncate">{name}</span>
      {onRemove ? (
        <button
          type="button"
          aria-label={`移除标签 ${name}`}
          className="ml-0.5 shrink-0 rounded-sm text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
          onClick={onRemove}
        >
          <X className="size-3" />
        </button>
      ) : null}
    </span>
  );
}

export function TagEditor({ documentID, tags }: { documentID: string; tags: Tag[] }) {
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const [input, setInput] = useState("");
  const allTags = useQuery({ queryKey: ["tags"], queryFn: listTags, enabled: open });
  const mutation = useMutation({
    mutationFn: (names: string[]) => setDocumentTags(documentID, names),
    // 同 scope 的 mutation 串行执行,保证整组替换的 PUT 按提交顺序到达服务端。
    scope: { id: `document-tags-${documentID}` },
    onSuccess: (updated) => {
      // 接口是整组替换语义,用服务端返回值立即回写缓存,
      // 避免连续增删时基于陈旧 props 计算出丢数据的列表。
      queryClient.setQueryData<Document>(["document", documentID], (old) =>
        old ? { ...old, tags: updated } : old,
      );
      queryClient.invalidateQueries({ queryKey: ["document", documentID] });
      queryClient.invalidateQueries({ queryKey: ["documents"] });
      queryClient.invalidateQueries({ queryKey: ["tags"] });
    },
  });

  // 请求在途时以已提交的列表为基准,连续操作不会互相覆盖。
  const names = mutation.isPending && mutation.variables ? mutation.variables : tags.map((tag) => tag.name);
  const has = (name: string) => names.some((item) => item.toLowerCase() === name.toLowerCase());
  const suggestions = (allTags.data ?? []).filter((tag) => !has(tag.name));

  function addTag(name: string) {
    const trimmed = name.trim();
    if (!trimmed || has(trimmed)) return;
    mutation.mutate([...names, trimmed]);
  }

  function removeTag(name: string) {
    mutation.mutate(names.filter((item) => item.toLowerCase() !== name.toLowerCase()));
  }

  function onInputKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Enter") {
      event.preventDefault();
      addTag(input);
      setInput("");
    }
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button variant="outline" size="sm">
          <TagIcon className="size-4" />
          标签
          {names.length ? <span className="text-xs text-muted-foreground">{names.length}</span> : null}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-80 p-3">
        <div className="space-y-3">
          <div className="text-sm font-medium">文档标签</div>
          {names.length ? (
            <div className="flex flex-wrap gap-1.5">
              {names.map((name) => (
                <TagChip key={name} name={name} onRemove={() => removeTag(name)} />
              ))}
            </div>
          ) : (
            <div className="text-sm text-muted-foreground">暂无标签,输入名称添加。</div>
          )}
          <div className="flex gap-2">
            <Input
              value={input}
              placeholder="新标签,回车添加"
              className="h-8"
              onChange={(event) => setInput(event.target.value)}
              onKeyDown={onInputKeyDown}
            />
            <Button
              variant="secondary"
              size="sm"
              disabled={!input.trim() || mutation.isPending}
              onClick={() => {
                addTag(input);
                setInput("");
              }}
            >
              <Plus className="size-4" />
              添加
            </Button>
          </div>
          {suggestions.length ? (
            <>
              <Separator />
              <div className="space-y-1">
                <div className="text-xs text-muted-foreground">已有标签</div>
                <div className="flex max-h-36 flex-wrap gap-1.5 overflow-y-auto">
                  {suggestions.map((tag) => (
                    <button
                      key={tag.id}
                      type="button"
                      className="inline-flex h-6 items-center gap-1 rounded-md border px-2 text-xs text-muted-foreground transition-colors hover:border-primary/40 hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                      onClick={() => addTag(tag.name)}
                    >
                      <Check className="size-3 opacity-0" />
                      {tag.name}
                    </button>
                  ))}
                </div>
              </div>
            </>
          ) : null}
          {mutation.error ? <div className="text-xs text-destructive">{mutation.error.message}</div> : null}
        </div>
      </PopoverContent>
    </Popover>
  );
}
