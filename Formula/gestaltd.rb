# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.2"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.2/gestaltd-macos-arm64.tar.gz"
      sha256 "2e003b67a38ac5d62bcc86f0053a1ab51fcf05229ca8314819859896d48f1f39"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.2/gestaltd-macos-x86_64.tar.gz"
      sha256 "08d9ea12d06298816c0ce261c66d12be42de03dc6cb4cd4ea15499188a532c8c"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.2/gestaltd-linux-arm64.tar.gz"
      sha256 "51323f6b5a1d79a4d92641050f230ef1169defcf6340bdbe0fdb7175d5a88ee8"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.2/gestaltd-linux-x86_64.tar.gz"
      sha256 "67c8e5fb96510f6b1d6ffc02594d29f084f7e7dfaf9ea66b164588033e3b0efb"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
