import type { Metadata } from "next";
import Link from "next/link";
import { site } from "@/config/site";
import { H, LegalPage, P, UL } from "@/features/legal/LegalPage";

export const metadata: Metadata = { title: "Terms of Service", alternates: { canonical: "/terms" } };

export default function Page() {
  return (
    <LegalPage title="Terms of Service">
      <P>These terms govern your use of {site.name} (the “Service”), operated by {site.legalName}. By using the Service you agree to them.</P>
      <H>1. Eligibility</H>
      <P>The Service contains adult content. You must be at least 18 years old (or the age of majority where you live, if higher) and legally permitted to view adult material in your location. Do not use the Service if you do not meet these requirements.</P>
      <H>2. Your account</H>
      <UL>
        <li>Provide accurate information and keep your password secure.</li>
        <li>You are responsible for activity on your account.</li>
        <li>We may require additional age or identity verification, including for uploaders.</li>
      </UL>
      <H>3. Your content</H>
      <P>You keep ownership of what you upload. You grant us a non-exclusive licence to host, process, display and distribute it as needed to operate the Service. You confirm that you have the right to publish it, that everyone depicted is an adult, and that every person depicted has consented to its creation and publication.</P>
      <H>4. Prohibited conduct</H>
      <P>You must follow our <Link href="/community-guidelines" className="underline">Community Guidelines</Link>. In particular, content involving anyone under 18 or shared without consent is strictly prohibited and will be removed and, where required, reported to the authorities.</P>
      <H>5. Reporting and moderation</H>
      <P>We may remove content, restrict features or suspend accounts that violate these terms. You can report content using the Report option on any video, comment or channel.</P>
      <H>6. Copyright</H>
      <P>We respond to valid notices of copyright infringement. See our <Link href="/dmca" className="underline">Copyright / DMCA policy</Link>.</P>
      <H>7. Termination</H>
      <P>You may stop using the Service and delete your account at any time. We may suspend or terminate access for violations of these terms or the law.</P>
      <H>8. Disclaimers and liability</H>
      <P>The Service is provided “as is” to the extent permitted by law. [Insert limitation of liability, governing law and dispute-resolution terms appropriate to your jurisdiction.]</P>
      <H>9. Changes</H>
      <P>We may update these terms. Continued use after changes take effect means you accept them.</P>
      <H>10. Contact</H>
      <P>Questions: {site.supportEmail}</P>
    </LegalPage>
  );
}
