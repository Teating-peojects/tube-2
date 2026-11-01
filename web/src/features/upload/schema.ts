import { z } from "zod";
import { uploadCategories } from "@/config/upload";

// Client validation is for UX only; the backend validates again.
export const uploadSchema = z.object({
  title: z.string().trim().min(1, "Add a title").max(100, "At most 100 characters"),
  description: z.string().max(5000, "At most 5,000 characters"),
  category: z.enum(uploadCategories, { errorMap: () => ({ message: "Choose a category" }) }),
  tags: z.array(z.string().min(1).max(30)).max(15, "At most 15 tags"),
  visibility: z.enum(["public", "unlisted", "private"]),
  comments: z.enum(["on", "off", "review"]),
  language: z.string().min(2).max(10),
  ageGate: z.boolean(),
  consent: z.literal(true, { errorMap: () => ({ message: "Please confirm before publishing" }) }),
});
export type UploadValues = z.infer<typeof uploadSchema>;
