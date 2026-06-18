# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.21"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.21/gestaltd-macos-arm64.tar.gz"
      sha256 "5761abdd3e1d5dfd1c9983e3b934c71a83fe71bdbf3ebaa09211f9dfa8a0d0c4"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.21/gestaltd-macos-x86_64.tar.gz"
      sha256 "a62592b9cb70d9006a540af989cae282979436d4578ebbbd5584d361be4b143d"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.21/gestaltd-linux-arm64.tar.gz"
      sha256 "af0f54c43827a3d639327d8640d8d48314a68d5b41936eb8497a199fa8d4aadc"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.21/gestaltd-linux-x86_64.tar.gz"
      sha256 "921cd0a8ba06fb17e04d81bd0ce3db229689e55d2cd0b8019f17f17ab14ae4a2"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
