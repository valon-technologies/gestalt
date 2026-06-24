# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.31"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.31/gestaltd-macos-arm64.tar.gz"
      sha256 "209ba84aa49f0d4c7441467e8dbc6f5f0e56e4c73dbd19b27ae4bfd5a34b7c08"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.31/gestaltd-macos-x86_64.tar.gz"
      sha256 "65df3b992c4a704ad9000677cfb2dc5fbce9f64550e58e0c41b80db519150eeb"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.31/gestaltd-linux-arm64.tar.gz"
      sha256 "a4ba0e481843aeabf31a9b84292350c286f7615f2e3a5e59e89d5cf83801190d"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.31/gestaltd-linux-x86_64.tar.gz"
      sha256 "504c144d2f6860949c1c3b986784990ba8e7cff3447e75f0c9207dae980f101d"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
